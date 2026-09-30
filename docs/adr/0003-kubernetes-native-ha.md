# 3. Kubernetes-native high availability

Date: 2026-09-28

## Status

Accepted (fork `k8s-native`)

## Context

Upstream Atlantis runs as a single replica. Its state lives in BoltDB (one
file, one process) or Redis, and much of its runtime state is held in the
process itself:

- git clones and plan files on local disk,
- the in-memory `WorkingDirLocker`,
- live job output and websocket streams,
- the cancellation tracker.

A second replica behind the same Service can therefore corrupt work or lose
it: two pods plan the same pull, `atlantis apply` lands on a pod that has no
plan, and job links 404.

We want Atlantis to run as an ordinary Deployment with N replicas, survive
node loss and rolling updates, and need no external database.

## Decision

1. **State in the Kubernetes API.** `--locking-db-type=kubernetes` implements
   `db.Database` with `coordination.k8s.io/v1` Leases and an
   `atlantis.runatlantis.io/v1alpha1` `PullStatus` CRD:
   - Project locks and the global apply lock are Leases. Creating one
     acquires the lock, which is atomic across replicas. Deletes carry UID and
     resourceVersion preconditions.
   - Pull status uses optimistic concurrency with jittered conflict retries.
   - A shared conformance suite (`server/core/db/dbtest`) keeps every backend
     behaviourally identical.
2. **Pull affinity.** Every replica renews a member Lease. Each pull is owned
   by one live, non-draining member, chosen by rendezvous hashing, so a
   membership change moves only the departing member's pulls.
   - After a replica validates and parses a webhook, it forwards the typed
     command to the owner over an internal, token-authenticated listener
     (`--cluster-port`). Clones, job output and cancellation therefore stay on
     one pod.
   - If the owner is unreachable, the command runs locally.
3. **Defence in depth.** A per-pull Lease (`pulllock`) wraps the in-process
   `WorkingDirLocker`. Two replicas that briefly both think they own a pull
   during a membership change still cannot run commands for it concurrently.
4. **Leader election** (client-go `LeaseLock`) runs cluster-wide
   housekeeping: it garbage-collects Leases of replicas that died without
   cleaning up.
5. **Graceful drain.** On SIGTERM a replica marks its member Lease as
   draining, so it stops owning pulls immediately. It also fails `/readyz`,
   finishes in-flight jobs (which stay reachable through the job proxy), then
   releases its Leases.
6. **Observability.**
   - OpenTelemetry spans cover the webhook, the command, each project and each
     workflow step, and forwarded commands. W3C context is propagated between
     replicas.
   - Logs carry `trace_id` and `span_id`.
   - Tally/Prometheus metrics cover membership, leadership, routing and plan
     risk.

7. **Hardening after review** (Jev-verified findings):
   - Readiness no longer probes the shared API server.
   - Pull locks are fenced: a replica stops trusting its lease after half its
     duration without a renewal, and checks it before every workflow step.
   - Forwarded commands carry a request ID, and are never re-run locally when
     delivery is ambiguous and the owner is alive.
   - The chart uses a StatefulSet with per-replica volumes, and applies are
     routed to the replica that holds the plan (`PullStatus.spec.plannedBy`),
     waiting for it while it restarts.

## Consequences

- No BoltDB or Redis is needed. RBAC is namespace-scoped: Leases and
  PullStatuses only.
- A plan is applied on the replica that made it. Restarts keep it; losing
  the replica's volume or scaling it away loses it unless the S3 plan store
  (`external_stores`) is configured.
- Former owners keep stale clones on their volume until the pull is closed
  or the volume is recycled. The volume is bounded by `persistence.size`.
- Some data is still per replica:
  - drift-detection results,
  - the index page's list of running jobs, which shows only jobs on the
    replica that served the page.
- Scaling changes ownership. Prefer a fixed replica count; the chart's HPA
  scales down slowly.

## Follow-ups

- M5, done: the BoltDB and Redis backends and `--locking-db-type` were removed;
  Kubernetes is the only storage backend.
- Store drift results in a CRD.
- Aggregate the job list across replicas.

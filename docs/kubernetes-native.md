# Kubernetes-native Atlantis

This fork runs Atlantis as a highly available Deployment. Locks, pull status,
membership and leader election are all stored as Kubernetes objects. It needs
no BoltDB, Redis or persistent volume. The design and its trade-offs are in
[ADR 0003](adr/0003-kubernetes-native-ha.md).

## Install

```bash
kubectl create namespace atlantis
kubectl -n atlantis create secret generic atlantis-vcs \
  --from-literal=ATLANTIS_GH_USER=my-bot \
  --from-literal=ATLANTIS_GH_TOKEN=... \
  --from-literal=ATLANTIS_GH_WEBHOOK_SECRET=...
helm install atlantis deploy/helm/atlantis -n atlantis \
  --set vcsSecretName=atlantis-vcs \
  --set atlantisUrl=https://atlantis.example.com \
  --set repoAllowlist='github.com/example-org/*'
```

The chart installs the `PullStatus` CRD. It also installs a namespaced Role
limited to Leases and PullStatuses, a cluster token Secret, a
PodDisruptionBudget, and a NetworkPolicy that limits the cluster port to
Atlantis pods. Replicas are spread across nodes and zones. Optional pieces are
an Ingress, an HPA, a ServiceMonitor, a PrometheusRule and OTLP tracing.

Without Helm, the server flags are:

```bash
atlantis server \
  --locking-db-type=kubernetes \
  --cluster-token=$ATLANTIS_CLUSTER_TOKEN   # plus POD_NAME, POD_NAMESPACE, POD_IP from the downward API
```

## How it works

| Concern | Mechanism |
| --- | --- |
| Project locks, global apply lock | `Lease` objects named `lock-*` / `cmdlock-*`. Creating one acquires the lock atomically. |
| Plan/apply status per pull | `PullStatus` objects (`kubectl get pullstatuses`), updated with optimistic concurrency. |
| Which replica handles a pull | Member Leases (`member-*`) plus rendezvous hashing. Webhooks are forwarded to the owner over `--cluster-port`. |
| Two replicas on one pull | Per-pull Lease (`pulllock-*`), re-entrant within a replica and renewed while commands run. |
| Job pages and websockets | Proxied to whichever replica has the job. |
| Housekeeping | The leader (`atlantis-leader` Lease) deletes Leases left by crashed replicas. |
| Rolling update / scale down | The terminating replica marks itself draining. Its pulls move immediately, `/readyz` fails, and running jobs finish before it exits. |

To inspect the cluster state:

```bash
kubectl -n atlantis get leases -l app.kubernetes.io/managed-by=atlantis -L atlantis.runatlantis.io/type
kubectl -n atlantis get pullstatuses -o wide
kubectl -n atlantis get lease atlantis-leader -o jsonpath='{.spec.holderIdentity}'
```

Nothing written to the API server contains VCS credentials: clone URLs and
pull request bodies are stripped before storing.

## Monitoring

To expose metrics, keep the `metrics.prometheus` block in the chart's
`repoConfig`, then enable `metrics.serviceMonitor`. The metrics are:

| Metric | Meaning |
| --- | --- |
| `atlantis_cluster_members` | Live replicas seen by this replica |
| `atlantis_cluster_leader` | 1 on the leader |
| `atlantis_cluster_routing_forward_success` / `_forward_error` / `_local` | Command routing outcomes |
| `atlantis_plan_risk_assessed{tier}` / `atlantis_plan_risk_error` | Plan risk results |
| `atlantis_cmd_*`, `atlantis_api_*` | Existing Atlantis command metrics |

`metrics.prometheusRule.enabled=true` adds alerts for:

- no leader,
- degraded membership,
- forwarding errors,
- plan risk errors.

## Logging and tracing

Logs are JSON. Command logs include `repo`, `pull`, `replica`, `trace_id` and
`span_id`, so a log line can be followed to its trace.

With `--tracing-enabled` (chart: `tracing.enabled`, `tracing.endpoint`),
Atlantis exports OTLP/gRPC spans. A command produces this span tree:

```
POST /events                          (webhook, on the receiving replica)
└─ atlantis.cluster.forward           (if another replica owns the pull)
   └─ atlantis.internal               (on the owner)
      └─ atlantis.command.plan
         └─ atlantis.project.plan     (repo, pull, project, workspace, dir)
            ├─ atlantis.step.init
            ├─ atlantis.step.plan
            └─ atlantis.plan_risk
```

The exporter and sampler are configured with the standard `OTEL_*`
environment variables.

## Plan risk (TypeSafe Jev)

See [plan-risk.md](plan-risk.md).

## Limitations

- **Plans are applied on the replica that made them.** If that replica is
  lost, the user must re-plan unless the S3 plan store (`external_stores`) is
  configured.
- **Stale clones.** A replica that loses ownership keeps its clones until it
  restarts. They live on `emptyDir`, bounded by `dataVolume.sizeLimit`.
- **Per-replica data.**
  - Drift detection results.
  - The index page's list of running jobs. Locks on the index page are
    cluster-wide.
- **Object size.** A `PullStatus` object is limited by the API server's object
  size (about 1.5 MiB), which is thousands of projects per pull.

## Roadmap

- **M5: remove BoltDB and Redis.** They are kept for now so the fork can merge
  from upstream and so single-replica users can migrate. The removal is:
  1. Make `kubernetes` the default `--locking-db-type`.
  2. Add a one-shot `atlantis migrate-db --from=boltdb|redis` that copies
     locks and pull status into Leases/PullStatuses.
  3. Delete `server/core/boltdb`, `server/core/redis`, their flags, docs and
     `docker-compose` service, and the `go.etcd.io/bbolt`, `go-redis` and
     `miniredis` dependencies.
  4. Port the e2e tests from BoltDB to a fake-client KubeDB. The conformance
     suite already guarantees identical behaviour.
- Store drift results as a CRD and aggregate the job list across replicas.

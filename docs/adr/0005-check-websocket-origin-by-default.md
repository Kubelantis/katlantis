# 5. Check the websocket origin by default

Date: 2026-10-27

## Status

Proposed

## Context

Job pages stream output over a websocket at `/jobs/{id}/ws`. `--websocket-check-origin` makes the server refuse a websocket whose `Origin` host differs from the request's `Host` (Gorilla's same-origin check; requests with no `Origin` header are still accepted, but browsers always send one on websocket requests, so the check applies to every browser). It defaults to `false`, so any origin is accepted.

When the web UI is protected by `--web-basic-auth`, or by authentication at the Ingress that relies on cookies, the browser sends those credentials with a websocket opened by any page. A malicious site a logged-in user visits can then open the job websocket and read job output, which can include plan details (cross-site websocket hijacking). Without web authentication the output is readable anyway, so the check adds nothing there.

In this fork, job pages opened through one replica are proxied to the replica that ran the job. The proxy keeps the original `Host` header (`server/clusterrpc/jobproxy.go`), so the same-origin check works across replicas. See issue #8.

## Decision

Default `--websocket-check-origin` to `true` in the next minor release. The flag stays, so `--websocket-check-origin=false` restores the old behaviour.

Alternatives considered:

- **Keep `false` and recommend `true` in the docs.** Most deployments never change defaults, so they stay exposed.
- **An allowlist of origins.** More flexible for setups that embed job pages elsewhere, but no current use needs it. It could be added later as a separate flag.

## Consequences

- Deployments with web authentication are protected against cross-site websocket hijacking without any configuration.
- Breaking for setups where the browser's origin differs from the `Host` the server sees: a proxy that rewrites `Host` to an internal name, or a job page embedded in another site. Live job output then stops streaming until `Host` is preserved or the flag is set to `false`. The release notes must say so.

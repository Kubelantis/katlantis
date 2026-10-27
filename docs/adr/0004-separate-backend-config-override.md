# 4. Separate override for repo-side backend_config

Date: 2026-10-27

## Status

Proposed

## Context

Native inputs let server-side config set `var_files`, `vars`, `backend_config`, `env` and `extra_args` per project. An operator lets a repo set its own inputs in `atlantis.yaml` by listing `inputs` in `allowed_overrides`. That one key grants all five. For `backend_config`, the repo's value replaces the server's (`env` and `extra_args` are merged).

So any repo trusted to choose its own var files or env vars can also choose the backend that `terraform init` uses. A pull request can then:

- plan against different state, making a plan look harmless;
- once applied, write state, including sensitive attribute values, to a backend it chose, using the server's cloud credentials.

This is documented, and the threat model limits it: `terraform plan` already runs pull request code with the server's credentials (for example through `data "external"`), and requiring approval before plan is the real boundary. But operators commonly grant `inputs` so teams can pick environments and var files, without meaning to delegate the choice of state backend. A Jev triage of remaining risks rated this a backlog item (severity 1.63 of 3). See issue #6.

## Decision

Add `backend_config` as its own `allowed_overrides` key:

```yaml
repos:
- id: /.*/
  allowed_overrides: [inputs]                  # var_files, vars, env, extra_args
- id: github.com/org/infra
  allowed_overrides: [inputs, backend_config]  # also inputs.backend_config
```

- A repo may set `inputs.backend_config` only when `backend_config` is allowed.
- Transition: for one minor release, `inputs` alone still allows `backend_config`, and Atlantis logs a deprecation warning naming the repo and project. After that, the config is rejected as for any key that isn't allowed.
- `atlantis migrate-workflows` adds `backend_config` to `allowed_overrides` when it moves a backend config into repo-side inputs, and reports it for review.

Alternatives considered:

- **A server-side allowlist of backend config values or files** (for example `allowed_backend_configs: [regex]`). It is finer-grained, but `backend_config` entries can be files in the pull request, whose contents the allowlist can't see, so it would only constrain file names. It could be added later on top of this decision.
- **Keep the status quo and document it.** This leaves the most common grant, `inputs`, wider than most operators intend.
- **Merge backend config instead of replacing it.** Terraform takes the last `-backend-config` value for a key, so a repo could still override every key.

## Consequences

- Operators can delegate environment selection without delegating the state backend.
- Breaking, after the transition release: repos that set `inputs.backend_config` under `allowed_overrides: [inputs]` must get `backend_config` added. The deprecation warning and the migrate tool point to it.
- One more override key to document and test.

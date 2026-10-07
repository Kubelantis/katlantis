# Upgrading atlantis.yaml

## Upgrading To Native Workflows

Every project now runs the built-in steps, configured with
[native inputs](server-side-repo-config.md#native-inputs) and
[`tool`](server-side-repo-config.md#terragrunt). These keys were removed, and a
config that still uses them fails with an error naming the key:

- `workflows`, and `workflow` on projects and server-side repo entries
- `allowed_workflows` and `allow_custom_workflows`
- `custom_policy_check`, and `workflow` or `custom_policy_check` in `allowed_overrides`
- `run`, `env` and `multienv` steps

`atlantis migrate-workflows` converts them; see
[Migrating Custom Workflows To Native Inputs](server-side-repo-config.md#migrating-custom-workflows-to-native-inputs).

```yaml
# Before
version: 3
projects:
- dir: envs/prod
  workflow: prod
workflows:
  prod:
    plan:
      steps:
      - init:
          extra_args: [-backend-config=prod.backend.hcl]
      - plan:
          extra_args: [-var-file=prod.tfvars]
```

```yaml
# After
version: 3
projects:
- dir: envs/prod
  inputs:                        # needs allowed_overrides: [inputs] on the server
    var_files: [prod.tfvars]
    backend_config: [prod.backend.hcl]
```

Commands that must run in Atlantis's clone of the pull request, such as
decrypting secrets, belong in server-side
[pre workflow hooks](pre-workflow-hooks.md) or
[post workflow hooks](post-workflow-hooks.md).

## Upgrading From v2 To v3

Versions 2 and 3 differed only in how custom `run` steps were parsed. Run steps
no longer exist, so both versions are read the same way and need no changes.

## Upgrading From V1 To V3

If you are upgrading from an **old** Atlantis version `<=v0.3.10` (from before July 4, 2018)
you'll need to follow the following steps.

### Single atlantis.yaml

If you had multiple `atlantis.yaml` files per directory then you'll need to
consolidate them into a single `atlantis.yaml` file at the root of the repo.

For example, if you had a directory structure:

```plain
.
├── project1
│   └── atlantis.yaml
└── project2
    └── atlantis.yaml
```

Then your new structure would look like:

```plain
.
├── atlantis.yaml
├── project1
└── project2
```

And your `atlantis.yaml` would look something like:

```yaml
version: 3
projects:
- dir: project1
  terraform_version: my-version
- dir: project2
  terraform_version: my-version
```

### Terraform Version

The `terraform_version` key moved from being a top-level key to being per `project`
so if before your `atlantis.yaml` was in directory `mydir` and looked like:

```yaml
terraform_version: 0.11.0
```

Then your new config would be:

```yaml
version: 3
projects:
- dir: mydir
  terraform_version: 0.11.0
```

### Extra Arguments

`extra_arguments` is now `extra_args` in
[native inputs](server-side-repo-config.md#native-inputs). Given a previous config:

```yaml
extra_arguments:
  - command_name: init
    arguments:
    - "-lock=false"
  - command_name: plan
    arguments:
    - "-lock=false"
  - command_name: apply
    arguments:
    - "-lock=false"
```

Your config would now look like:

```yaml
version: 3
projects:
- dir: mydir
  inputs:                        # needs allowed_overrides: [inputs] on the server
    extra_args:
      init: ["-lock=false"]
      plan: ["-lock=false"]
      apply: ["-lock=false"]
```

### Pre/Post Commands

`pre_*` and `post_*` commands are not supported in `atlantis.yaml`. Commands
that must run in Atlantis's clone go in server-side
[pre workflow hooks](pre-workflow-hooks.md) or
[post workflow hooks](post-workflow-hooks.md), which run once per command at
the root of the repository:

```yaml
# repos.yaml
repos:
- id: /.*/
  pre_workflow_hooks:
  - run: curl http://example.com
    commands: plan
  post_workflow_hooks:
  - run: curl http://example.com
    commands: plan, apply
```

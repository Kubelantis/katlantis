# Pre Workflow Hooks

Pre workflow hooks run scripts in Atlantis's clone of the pull request before
Atlantis runs a command. They are defined in the [server-side repo
config](server-side-repo-config.md) only, so the Atlantis administrator
controls what runs.

1. Pre workflow hooks run once per command, at the root of the repository,
   before Atlantis reads the repository configuration.
2. Pre workflow hooks run outside of Atlantis commands, which means they do
   not surface their output back to the PR as a comment.

Projects themselves always run the built-in steps; configure them with
[native inputs](server-side-repo-config.md#native-inputs) and
[`tool`](server-side-repo-config.md#terragrunt).

## Usage

Pre workflow hooks can only be specified in the Server-Side Repo Config under the
`repos` key.

::: tip Note
By default, `pre-workflow-hooks` do not prevent Atlantis from executing its
workflows(`plan`, `apply`) even if a `run` command exits with an error. This
behavior can be changed by setting the [fail-on-pre-workflow-hook-error](server-configuration.md#fail-on-pre-workflow-hook-error)
flag in the Atlantis server configuration.
:::

## Atlantis Command Targeting

By default, the workflow hook will run when any command is processed by Atlantis.
This can be modified by specifying the `commands` key in the workflow hook containing a comma-delimited list
of Atlantis commands that the hook should be run for. Detail of the Atlantis commands
can be found in [Using Atlantis](using-atlantis.md).

### Example

```yaml
repos:
    - id: /.*/
      pre_workflow_hooks:
        - run: ./plan-hook.sh
          description: Plan Hook
          commands: plan
        - run: ./plan-apply-hook.sh
          description: Plan & Apply Hook
          commands: plan, apply
```

## Use Cases

Use a pre workflow hook for work that must happen in Atlantis's clone before
Terraform runs, for example:

- decrypting secrets committed to the repository (git-crypt, sops)
- rendering `backend.tf` or `provider.tf` from templates
- writing `.terraformrc` for a private registry or provider mirror
- installing a helper tool the configuration needs

### Dynamic Repo Config Generation

Terragrunt units and CDK Terrain stacks are discovered natively when the
repository's [`tool`](server-side-repo-config.md#terragrunt) is `terragrunt` or
`cdktn`, so they need no generated `atlantis.yaml`. For other layouts, a hook
can generate the repo `atlantis.yaml` right before Atlantis parses it:

```yaml
repos:
    - id: /.*/
      pre_workflow_hooks:
        - run: ./repo-config-generator.sh
          description: Generating configs
```

## Customizing the Shell

By default, the command will be run using the 'sh' shell with an argument of '-c'. This
can be customized using the `shell` and `shellArgs` keys.

Example:

```yaml
repos:
    - id: /.*/
      pre_workflow_hooks:
        - run: |
            echo "decrypting secrets"
            git-crypt unlock /secrets/git-crypt.key
          description: Decrypting secrets
          shell: bash
          shellArgs: -cv
```

## Reference

### Custom `run` Command

```yaml
- run: custom-command
```

| Key | Type | Default | Required | Description |
| --- | --- | --- | --- | --- |
| run | string | none | no | Run a custom command |
| description | string | none | no | Pre hook description |
| shell | string | 'sh' | no | The shell to use for running the command |
| shellArgs | string | '-c' | no | The shell arguments to use for running the command |

::: tip Notes

* `run` commands are executed with the following environment variables:
  * `BASE_REPO_NAME` - Name of the repository that the pull request will be merged into, ex. `atlantis`.
  * `BASE_REPO_OWNER` - Owner of the repository that the pull request will be merged into, ex. `runatlantis`.
  * `HEAD_REPO_NAME` - Name of the repository that is getting merged into the base repository, ex. `atlantis`.
  * `HEAD_REPO_OWNER` - Owner of the repository that is getting merged into the base repository, ex. `acme-corp`.
  * `HEAD_BRANCH_NAME` - Name of the head branch of the pull request (the branch that is getting merged into the base)
  * `HEAD_COMMIT` - The sha256 that points to the head of the branch that is being pull requested into the base. If the pull request is from Bitbucket Cloud the string will only be 12 characters long because Bitbucket Cloud truncates its commit IDs.
  * `BASE_BRANCH_NAME` - Name of the base branch of the pull request (the branch that the pull request is getting merged into)
  * `PULL_NUM` - Pull request number or ID, ex. `2`.
  * `PULL_URL` - Pull request URL, ex. `https://github.com/runatlantis/atlantis/pull/2`.
  * `PULL_AUTHOR` - Username of the pull request author, ex. `acme-user`.
  * `DIR` - The absolute path to the root of the cloned repository.
  * `USER_NAME` - Username of the VCS user running command, ex. `acme-user`. During an autoplan, the user will be the Atlantis API user, ex. `atlantis`.
  * `COMMENT_ARGS` - Any additional flags passed in the comment on the pull request. Flags are separated by commas and
      every character is escaped, ex. `atlantis plan -- arg1 arg2` will result in `COMMENT_ARGS=\a\r\g\1,\a\r\g\2`.
  * `COMMAND_NAME` - The name of the command that is being executed, i.e. `plan`, `apply` etc.
  * `OUTPUT_STATUS_FILE` - An output file to customize the success or failure status. ex. `echo 'failure' > $OUTPUT_STATUS_FILE`.
  * `PROJECT_NAME` - Project name passed by the `-p` option. If `-p` is not provided, this value is empty.

:::

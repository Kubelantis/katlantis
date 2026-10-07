# Post Workflow Hooks

Post workflow hooks run scripts in Atlantis's clone of the pull request after
Atlantis has run a command. They are defined in the [server-side repo
config](server-side-repo-config.md) only, so the Atlantis administrator
controls what runs. They run outside of Atlantis commands, which means they do
not surface their output back to the PR as a comment.

## Usage

Post workflow hooks can only be specified in the Server-Side Repo Config under
the `repos` key.

## Atlantis Command Targeting

By default, the workflow hook will run when any command is processed by Atlantis.
This can be modified by specifying the `commands` key in the workflow hook containing a comma-delimited list
of Atlantis commands that the hook should be run for. Detail of the Atlantis commands
can be found in [Using Atlantis](using-atlantis.md).

### Example

```yaml
repos:
    - id: /.*/
      post_workflow_hooks:
        - run: ./plan-hook.sh
          description: Plan Hook
          commands: plan
        - run: ./plan-apply-hook.sh
          description: Plan & Apply Hook
          commands: plan, apply
```

## Use Cases

Use a post workflow hook for work that needs what Atlantis produced in its
clone, for example:

- estimating cost from the plan
- uploading the plan JSON for audit
- running a scanner on the plan for visibility (a hook cannot block apply;
  use [Conftest policy checking](policy-checking.md) for that)

### Cost estimation reporting

In this example a post workflow hook generates cost estimates for the
repository with [Infracost](https://www.infracost.io/docs/integrations/cicd/#cicd-integrations)
after each plan.

```yaml
# repos.yaml
repos:
  - id: /.*/
    post_workflow_hooks:
      - run: |
          infracost breakdown --path=. --format=json --out-file=/tmp/$BASE_REPO_OWNER-$BASE_REPO_NAME-$PULL_NUM-infracost.json
          infracost output --path=/tmp/$BASE_REPO_OWNER-$BASE_REPO_NAME-$PULL_NUM-infracost.json --format=github-comment --out-file=/tmp/infracost-comment.md
        description: Running infracost
        commands: plan
      # Now report the output as desired, e.g. post to GitHub as a comment.
      # ...
```

## Customizing the Shell

By default, the commands will be run using the 'sh' shell with an argument of '-c'. This
can be customized using the `shell` and `shellArgs` keys.

Example:

```yaml
repos:
    - id: /.*/
      post_workflow_hooks:
        - run: |
            echo 'atlantis.yaml config:'
            cat atlantis.yaml
          description: atlantis.yaml report
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
| description | string | none | no | Post hook description |
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
  * `COMMAND_HAS_ERRORS` - Indicates whether any errors occurred during the execution of the command (`plan`, `apply`). If set to `true`, at least one error was encountered; otherwise, it is `false`.
  * `OUTPUT_STATUS_FILE` - An output file to customize the success or failure status. ex. `echo 'failure' > $OUTPUT_STATUS_FILE`.
  * `PROJECT_NAME` - Project name passed by the `-p` option. If `-p` is not provided, this value is empty.
:::

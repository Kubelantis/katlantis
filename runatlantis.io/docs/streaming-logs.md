# Real-time logs

Atlantis supports streaming terraform logs in real time by default. Currently, only two commands are supported

* atlantis plan
* atlantis apply

::: warning
Not all terraform commands are supported. Projects that use [Terragrunt](server-side-repo-config.md#terragrunt) or [CDK Terrain](server-side-repo-config.md#cdk-terrain) stream their output too.
:::

In order to view real-time terraform logs, a user can navigate through the *details* section of a given project's plan or apply status check.

![Plan Command](./images/plan.png)

This will link to the Atlantis UI which provides real-time logging in addition to native terraform syntax highlighting.

![Plan Output](./images/plan_output.png)

::: warning
As of now the logs are currently stored in memory and cleared when a given pull request is closed, so this link shouldn't be persisted anywhere.
:::

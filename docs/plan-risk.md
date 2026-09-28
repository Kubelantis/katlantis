# Plan risk with TypeSafe Jev

With `--plan-risk-enabled`, Atlantis rates every plan as `low`, `medium`,
`high` or `critical` and posts the rating in the plan comment. Applies of
plans above `--plan-risk-max-unapproved-tier` (default `low`) then require an
approved pull request.

```
**Plan risk:** :orange_circle: **high** (0 to create, 1 to update, 1 to delete, 0 to replace)
* `aws_db_instance.orders` may destroy stored data
```

## What is sent

Atlantis runs `terraform show -json` on the plan and sends only:

- the project's repository, directory, workspace and name,
- each managed resource change's address, type and action.

Attribute values are never sent because they can contain secrets. Data
sources and no-op changes are dropped.

## How the tier is decided

Code does the parsing, counting and policy. [Jev](https://docs.typesafe.ai)
answers only semantic questions, all in a single request:

| Question | Type | Asked for |
| --- | --- | --- |
| Would this delete/replace permanently destroy stored data? | Noul | each delete or replace |
| Does this change a security boundary (IAM, keys, firewall, public exposure)? | Noul | each change |
| Does the project target production? | Noul | the plan |
| How much of the system could a failure affect? | Score, 4 levels | the plan |

Code then maps the answers to a tier (Noul threshold 0.5, blast radius 0–3):

| Tier | When |
| --- | --- |
| critical | data loss **and** production, or blast radius ≥ 2.5 |
| high | data loss, or security change in production, or blast radius ≥ 1.75, or destructive changes beyond the 40 assessed |
| medium | any delete/replace, any security change, a production update, blast radius ≥ 0.75, or unassessed changes |
| low | everything else, including plans with no changes |

Code rules set a floor the model can raise but never lower: any delete is at
least `medium`. If the assessment fails for any reason (no `show` output, an
API error, a missing answer), the tier falls back to
`--plan-risk-failure-tier` (default `high`) and the comment says so.

These thresholds are starting points. Review `atlantis_plan_risk_assessed{tier}`
and the findings on real pull requests before tightening
`--plan-risk-max-unapproved-tier`. Pin `--typesafe-model` (default
`jev-1.13.0`) so tiers do not change when a new model is released.

## Configuration

| Flag | Default |
| --- | --- |
| `--plan-risk-enabled` | `false` |
| `--typesafe-api-key` (`ATLANTIS_TYPESAFE_API_KEY`) | required when enabled |
| `--typesafe-model` | `jev-1.13.0` |
| `--typesafe-api-url` | `https://api.typesafe.ai` |
| `--plan-risk-max-unapproved-tier` | `low` |
| `--plan-risk-failure-tier` | `high` |

The gate applies to every project and cannot be disabled in repo config.
Opted-in API applies without a pull request (drift remediation) skip it, as
they skip the other pull request requirements.

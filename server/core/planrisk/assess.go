// Package planrisk scores the risk of a Terraform plan so risky applies can
// require review.
//
// The work is split the way System One models are meant to be used:
//   - Code parses `terraform show -json`, counts actions, orders changes by
//     severity, and applies the tier policy. The model never counts or does
//     arithmetic.
//   - Jev answers narrow semantic questions in one request: per destructive
//     change, would it destroy stored data; per change, does it touch a
//     security boundary; does the project look like production; and how wide
//     is the blast radius.
//   - Code-level rules set a floor the model can raise but never lower: a
//     plan that deletes anything is at least medium.
//
// Only resource addresses, types, and actions are sent. Attribute values
// never leave Atlantis because they can contain secrets.
package planrisk

import (
	"context"
	"fmt"
	"time"

	tally "github.com/uber-go/tally/v4"

	"github.com/runatlantis/atlantis/server/events/models"
)

// Default thresholds. They are starting points; evaluate them on your own
// plans before relying on them (see docs/plan-risk.md).
const (
	noulThreshold     = 0.5
	blastMedium       = 0.75
	blastHigh         = 1.75
	blastCritical     = 2.5
	defaultMaxChanges = 40
)

// Project identifies what is being planned. It is part of the model state.
type Project struct {
	Repository string `json:"repository"`
	Directory  string `json:"directory"`
	Workspace  string `json:"workspace"`
	Name       string `json:"name,omitempty"`
}

// Assessor produces a PlanRisk for a plan.
type Assessor struct {
	Evaluator Evaluator
	// FailureTier is used when the plan cannot be assessed.
	FailureTier models.PlanRiskTier
	// MaxChanges bounds how many changes are judged individually.
	MaxChanges int
	Timeout    time.Duration
	Now        func() time.Time
	// Scope receives assessed{tier} and error counters; optional.
	Scope tally.Scope
}

type state struct {
	Project Project  `json:"project"`
	Changes []Change `json:"changes"`
}

var blastLevels = []string{
	"Isolated: only additive changes or a single low-importance resource; a mistake affects nothing else.",
	"Limited: a few resources belonging to one service or component; a mistake is contained and easy to roll back.",
	"Broad: shared infrastructure or several services, such as networks, clusters, identity and access, load balancers, or shared databases.",
	"Critical: foundational infrastructure, where a mistake causes a widespread outage or loss of data across many services.",
}

func questions(s state) map[string]Question {
	qs := map[string]Question{
		"production": {
			Type:         "noul",
			Instructions: "Do `project` or the addresses in `changes` indicate that this plan targets a production environment?",
			Criteria: map[string]string{
				"true":  "The repository, directory, workspace, project name, or resource addresses name a production or live customer-facing environment, e.g. prod, production, prd, live.",
				"false": "They name a non-production environment such as dev, test, qa, staging, sandbox, or preview, or give no signal about the environment.",
			},
		},
		"blast_radius": {
			Type:         "score",
			Instructions: "If applying the resource changes in `changes` went wrong, how much of the system could be affected?",
			Criteria:     blastLevels,
		},
	}
	for i, c := range s.Changes {
		qs[fmt.Sprintf("security_%d", i)] = Question{
			Type:         "noul",
			Instructions: fmt.Sprintf("Does `changes[%d]` (a %s of a %s resource) change a security boundary?", i, c.Action, c.Type),
			Criteria: map[string]string{
				"true":  "It changes identity and access management (users, roles, policies, permissions, service accounts), encryption keys or secrets, firewall or security group rules, or whether a service is publicly reachable.",
				"false": "It changes compute, storage, configuration, monitoring, tags, or other resources that do not grant access, hold keys, or control network exposure.",
			},
		}
		if c.Action.destructive() {
			qs[fmt.Sprintf("data_loss_%d", i)] = Question{
				Type:         "noul",
				Instructions: fmt.Sprintf("Would the %s of `changes[%d]` permanently destroy data stored in that resource?", c.Action, i),
				Criteria: map[string]string{
					"true":  "The resource stores persistent data that is not rebuilt automatically, e.g. a database, disk or volume, storage bucket, file system, queue or stream with retained messages, secret, key, DNS zone, or backup.",
					"false": "The resource stores no persistent data or is recreated without loss, e.g. a stateless instance, IAM binding, security rule, load balancer, route, or configuration object.",
				},
			}
		}
	}
	return qs
}

func (a *Assessor) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

// Assess scores a plan. It never returns nil: when assessment fails the
// result carries Error and the configured failure tier.
func (a *Assessor) Assess(ctx context.Context, project Project, showJSON []byte) *models.PlanRisk {
	risk := a.assess(ctx, project, showJSON)
	a.record(risk)
	return risk
}

func (a *Assessor) record(risk *models.PlanRisk) {
	if a.Scope == nil {
		return
	}
	a.Scope.Tagged(map[string]string{"tier": string(risk.Tier)}).Counter("assessed").Inc(1)
	if risk.Error != "" {
		a.Scope.Counter("error").Inc(1)
	}
}

func (a *Assessor) assess(ctx context.Context, project Project, showJSON []byte) *models.PlanRisk {
	risk := &models.PlanRisk{AssessedAt: a.now().UTC()}
	changes, err := ParseChanges(showJSON)
	if err != nil {
		return a.fail(risk, err)
	}
	for _, c := range changes {
		switch c.Action {
		case ActionCreate:
			risk.Creates++
		case ActionUpdate:
			risk.Updates++
		case ActionDelete:
			risk.Deletes++
		case ActionReplace:
			risk.Replaces++
		}
	}
	if len(changes) == 0 {
		risk.Tier = models.PlanRiskLow
		return risk
	}

	limit := a.MaxChanges
	if limit == 0 {
		limit = defaultMaxChanges
	}
	assessed := changes
	if len(assessed) > limit {
		assessed = assessed[:limit]
	}
	s := state{Project: project, Changes: assessed}

	timeout := a.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resp, err := a.Evaluator.Evaluate(ctx, s, questions(s))
	if err != nil {
		return a.fail(risk, err)
	}
	risk.Model = resp.Model

	answer := func(id string) (Answer, error) {
		ans, ok := resp.Answers[id]
		if !ok {
			return Answer{}, fmt.Errorf("TypeSafe response is missing answer %q", id)
		}
		return ans, nil
	}
	prod, err := answer("production")
	if err != nil {
		return a.fail(risk, err)
	}
	blast, err := answer("blast_radius")
	if err != nil {
		return a.fail(risk, err)
	}
	risk.BlastRadius = blast.Score

	isProd := prod.Noul >= noulThreshold
	dataLoss, security := false, false
	for i, c := range assessed {
		sec, err := answer(fmt.Sprintf("security_%d", i))
		if err != nil {
			return a.fail(risk, err)
		}
		if sec.Noul >= noulThreshold {
			security = true
			risk.Findings = append(risk.Findings, models.PlanRiskFinding{Address: c.Address, Action: string(c.Action), Reason: "changes a security boundary", Probability: sec.Noul})
		}
		if !c.Action.destructive() {
			continue
		}
		dl, err := answer(fmt.Sprintf("data_loss_%d", i))
		if err != nil {
			return a.fail(risk, err)
		}
		if dl.Noul >= noulThreshold {
			dataLoss = true
			risk.Findings = append(risk.Findings, models.PlanRiskFinding{Address: c.Address, Action: string(c.Action), Reason: "may destroy stored data", Probability: dl.Noul})
		}
	}
	if isProd {
		risk.Findings = append(risk.Findings, models.PlanRiskFinding{Reason: "targets a production environment", Probability: prod.Noul})
	}

	unassessed := changes[len(assessed):]
	unassessedDestructive := false
	for _, c := range unassessed {
		if c.Action.destructive() {
			unassessedDestructive = true
		}
	}
	if len(unassessed) > 0 {
		risk.Findings = append(risk.Findings, models.PlanRiskFinding{Reason: fmt.Sprintf("%d changes were not assessed individually", len(unassessed))})
	}

	destructive := risk.Deletes+risk.Replaces > 0
	switch {
	case (dataLoss && isProd) || blast.Score >= blastCritical:
		risk.Tier = models.PlanRiskCritical
	case dataLoss || (security && isProd) || blast.Score >= blastHigh || unassessedDestructive:
		risk.Tier = models.PlanRiskHigh
	case destructive || security || (isProd && risk.Updates > 0) || blast.Score >= blastMedium || len(unassessed) > 0:
		risk.Tier = models.PlanRiskMedium
	default:
		risk.Tier = models.PlanRiskLow
	}
	return risk
}

// Failure returns the result for a plan that could not be assessed.
func (a *Assessor) Failure(err error) *models.PlanRisk {
	risk := a.fail(&models.PlanRisk{AssessedAt: a.now().UTC()}, err)
	a.record(risk)
	return risk
}

func (a *Assessor) fail(risk *models.PlanRisk, err error) *models.PlanRisk {
	risk.Tier = a.FailureTier
	if risk.Tier == "" {
		risk.Tier = models.PlanRiskHigh
	}
	risk.Error = err.Error()
	return risk
}

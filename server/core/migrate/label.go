package migrate

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/runatlantis/atlantis/server/core/typesafe"
)

// Categories a removed command can fall into, with what replaces it.
var suggestions = map[string]string{
	"placeholder":       "Nothing: it only prints text.",
	"terragrunt":        "Set `tool: terragrunt` on the server-side repo entry: Atlantis discovers each unit as a project with dependency-aware autoplan, replacing `run --all` and terragrunt-atlantis-config.",
	"cdktf":             "CDK for Terraform is archived: move to CDK Terrain and set `tool: cdktn`, which synthesizes the app and makes each stack a project; or run `cdktf synth --hcl` once and commit the Terraform files.",
	"policy_scanner":    "Write the checks as Conftest policies for the built-in policy_check, or run the scanner in CI.",
	"cost_estimation":   "Run cost estimation in CI, for example the Infracost GitHub Action.",
	"credentials":       "Use workload identity on the Atlantis ServiceAccount (for example IRSA) or fixed values in inputs.env.",
	"config_generation": "Generate the configuration in CI and commit it, or use Atlantis autodiscovery.",
	"notification":      "Use Atlantis webhooks or CI notifications.",
	"other_script":      "Run it in CI before or after Atlantis.",
}

var categoryCriteria = map[string]string{
	"placeholder":       "It is a run step whose output nothing uses: it only prints text. The command of an env step is never a placeholder, because its output becomes an environment variable for the other steps.",
	"terragrunt":        "It runs Terragrunt or a Terragrunt helper such as terragrunt-atlantis-config.",
	"cdktf":             "It runs CDK for Terraform or CDK Terrain, such as cdktf or cdktn get or synth, or installs their dependencies.",
	"policy_scanner":    "It runs a policy, security or compliance scanner other than the built-in Conftest step, such as Checkov, tfsec, Trivy or Terrascan, or a script that checks policies.",
	"cost_estimation":   "It runs a cost estimation tool such as Infracost.",
	"credentials":       "It fetches or configures credentials, tokens or cloud sessions.",
	"config_generation": "It generates Atlantis or Terraform configuration files.",
	"notification":      "It sends notifications or calls external services to report status.",
	"other_script":      "It does something else.",
}

// Labeler labels removed commands so the report can say what replaces them.
type Labeler interface {
	Label(ctx context.Context, notes []Note) error
}

// JevLabeler asks TypeSafe Jev to categorise each removed command.
type JevLabeler struct {
	Evaluator typesafe.Evaluator
	// MinConfidence below which a label is reported as uncertain.
	MinConfidence float64
}

// scope groups notes from the same workflow or repo entry, so each command
// is judged together with its neighbours.
func scope(n Note) string {
	loc := n.Location
	if i := strings.Index(loc, " "); i > 0 {
		loc = loc[:i]
	}
	return n.File + "|" + loc
}

// Label implements Labeler. Each removed command is asked about once, with
// the step it came from and the other commands of the same workflow or repo
// entry as context (an env command is only meaningful next to them).
func (l *JevLabeler) Label(ctx context.Context, notes []Note) error {
	neighbours := map[string][]string{}
	for _, n := range notes {
		if n.Action == Removed && n.Command != "" {
			neighbours[scope(n)] = append(neighbours[scope(n)], n.Command)
		}
	}
	type result struct {
		label string
		conf  float64
	}
	var (
		mu      sync.Mutex
		results = map[int]result{}
		errs    []error
		wg      sync.WaitGroup
		sem     = make(chan struct{}, 8)
		asked   int
	)
	q := map[string]typesafe.Question{"category": {
		Type:         "choice",
		Instructions: "What is the purpose of `step.command` in an Atlantis Terraform workflow? Use `step.where`, `step.detail` and `step.workflow_commands` for context: an env step's command only computes a value for the steps around it.",
		Criteria:     categoryCriteria,
	}}
	for i, n := range notes {
		if n.Action != Removed || n.Command == "" {
			continue
		}
		asked++
		state := map[string]any{"step": map[string]any{
			"command":           n.Command,
			"where":             n.Location,
			"detail":            n.Detail,
			"workflow_commands": neighbours[scope(n)],
		}}
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			resp, err := l.Evaluator.Evaluate(ctx, state, q)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			a := resp.Answers["category"]
			results[i] = result{a.Choice, a.Confidence}
		})
	}
	wg.Wait()
	for i := range notes {
		r, ok := results[i]
		if !ok {
			continue
		}
		notes[i].Label, notes[i].Confidence = r.label, r.conf
		notes[i].Suggestion = suggestions[r.label]
		if r.conf < l.MinConfidence {
			notes[i].Suggestion = fmt.Sprintf("Uncertain (%.2f): %s", r.conf, notes[i].Suggestion)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("labelling %d of %d commands failed: %w", len(errs), asked, errs[0])
	}
	return nil
}

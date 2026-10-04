package valid_test

import (
	"regexp"
	"testing"

	"github.com/runatlantis/atlantis/server/core/config/valid"
	"github.com/runatlantis/atlantis/server/logging"
	. "github.com/runatlantis/atlantis/testing"
)

func cfgWithInputs(allowed []string) valid.GlobalCfg {
	g := valid.NewGlobalCfgFromArgs(valid.GlobalCfgArgs{})
	g.Repos = append(g.Repos, valid.Repo{
		IDRegex:          regexp.MustCompile(".*"),
		AllowedOverrides: allowed,
		Inputs:           &valid.Inputs{VarFiles: []string{"server.tfvars"}, Env: map[string]string{"TEAM": "platform"}},
	})
	return g
}

func planArgs(m valid.MergedProjectCfg) []string {
	for _, s := range m.Workflow.Plan.Steps {
		if s.StepName == "plan" {
			return s.ExtraArgs
		}
	}
	return nil
}

func TestMergeProjectCfgAppliesServerInputs(t *testing.T) {
	g := cfgWithInputs(nil)
	m := g.MergeProjectCfg(logging.NewNoopLogger(t), "github.com/org/repo", valid.Project{Dir: "."}, valid.RepoCfg{})
	Equals(t, []string{"-var-file=server.tfvars"}, planArgs(m))
	Equals(t, map[string]string{"TEAM": "platform"}, m.Env)

	d := g.DefaultProjCfg(logging.NewNoopLogger(t), "github.com/org/repo", ".", "default")
	Equals(t, []string{"-var-file=server.tfvars"}, planArgs(d))
	Equals(t, map[string]string{"TEAM": "platform"}, d.Env)
}

func TestMergeProjectCfgProjectInputsOverrideWhenAllowed(t *testing.T) {
	project := valid.Project{Dir: ".", Inputs: &valid.Inputs{VarFiles: []string{"prod.tfvars"}}}

	allowed := cfgWithInputs([]string{valid.InputsKey})
	m := allowed.MergeProjectCfg(logging.NewNoopLogger(t), "github.com/org/repo", project, valid.RepoCfg{})
	Equals(t, []string{"-var-file=prod.tfvars"}, planArgs(m))
	Equals(t, map[string]string{"TEAM": "platform"}, m.Env)

	notAllowed := cfgWithInputs(nil)
	m = notAllowed.MergeProjectCfg(logging.NewNoopLogger(t), "github.com/org/repo", project, valid.RepoCfg{})
	Equals(t, []string{"-var-file=server.tfvars"}, planArgs(m))
	err := notAllowed.ValidateRepoCfg(valid.RepoCfg{Projects: []valid.Project{project}}, "github.com/org/repo")
	ErrContains(t, "repo config not allowed to set 'inputs' key", err)
}

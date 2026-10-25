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

func TestMergeProjectCfgRepoInputArgsAreLiteral(t *testing.T) {
	project := valid.Project{Dir: ".", Inputs: &valid.Inputs{Vars: map[string]string{"t": "$ATLANTIS_GH_TOKEN"}}}
	g := cfgWithInputs([]string{valid.InputsKey})
	m := g.MergeProjectCfg(logging.NewNoopLogger(t), "github.com/org/repo", project, valid.RepoCfg{})
	Equals(t, []string{"-var-file=server.tfvars", "-var", "t=$ATLANTIS_GH_TOKEN"}, planArgs(m))
	Equals(t, []string{"-var", "t=$ATLANTIS_GH_TOKEN"}, m.LiteralArgs)

	// Server-side inputs alone stay expandable.
	m = g.MergeProjectCfg(logging.NewNoopLogger(t), "github.com/org/repo", valid.Project{Dir: "."}, valid.RepoCfg{})
	Equals(t, 0, len(m.LiteralArgs))
}

func TestValidateRepoCfgRejectsUnsafeRepoInputs(t *testing.T) {
	g := cfgWithInputs([]string{valid.InputsKey})
	cases := map[string]valid.Inputs{
		"inputs.env.LD_PRELOAD":              {Env: map[string]string{"LD_PRELOAD": "./x.so"}},
		"inputs.env.path":                    {Env: map[string]string{"path": "./bin"}},
		"inputs.env.TF_CLI_CONFIG_FILE":      {Env: map[string]string{"TF_CLI_CONFIG_FILE": "./tf.rc"}},
		"inputs.env.TF_CLI_ARGS_init":        {Env: map[string]string{"TF_CLI_ARGS_init": "-plugin-dir=./p"}},
		"inputs.env.TG_TF_PATH":              {Env: map[string]string{"TG_TF_PATH": "./evil"}},
		"inputs.env.HOME":                    {Env: map[string]string{"HOME": "."}},
		"inputs.env.TF_DATA_DIR":             {Env: map[string]string{"TF_DATA_DIR": "./data"}},
		"inputs.env.XDG_DATA_HOME":           {Env: map[string]string{"XDG_DATA_HOME": "./share"}},
		"inputs.env.XDG_DATA_DIRS":           {Env: map[string]string{"XDG_DATA_DIRS": "./share"}},
		"inputs.env.GIT_SSH_COMMAND":         {Env: map[string]string{"GIT_SSH_COMMAND": "./evil"}},
		"inputs.env.ATLANTIS_GH_TOKEN":       {Env: map[string]string{"ATLANTIS_GH_TOKEN": "x"}},
		"inputs.extra_args.policy_check":     {ExtraArgs: map[string][]string{"policy_check": {"--no-fail"}}},
		"inputs.extra_args.init cannot pass": {ExtraArgs: map[string][]string{"init": {"-plugin-dir=./p"}}},
	}
	for want, in := range cases {
		t.Run(want, func(t *testing.T) {
			err := g.ValidateRepoCfg(valid.RepoCfg{Projects: []valid.Project{{Dir: ".", Inputs: &in}}}, "github.com/org/repo")
			ErrContains(t, want, err)
		})
	}
}

func TestValidateRepoCfgAllowsOrdinaryRepoInputs(t *testing.T) {
	g := cfgWithInputs([]string{valid.InputsKey})
	in := valid.Inputs{
		VarFiles:  []string{"prod.tfvars"},
		Vars:      map[string]string{"region": "us-east-1"},
		Env:       map[string]string{"AWS_PROFILE": "prod", "TF_VAR_x": "1", "TF_LOG": "INFO"},
		ExtraArgs: map[string][]string{"plan": {"-refresh=false"}, "init": {"-upgrade"}},
	}
	Ok(t, g.ValidateRepoCfg(valid.RepoCfg{Projects: []valid.Project{{Dir: ".", Inputs: &in}}}, "github.com/org/repo"))
}

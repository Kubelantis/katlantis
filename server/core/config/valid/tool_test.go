package valid_test

import (
	"regexp"
	"testing"

	"github.com/runatlantis/atlantis/server/core/config/valid"
	"github.com/runatlantis/atlantis/server/logging"
	. "github.com/runatlantis/atlantis/testing"
)

func cfgWithTool(allowed []string, tools ...string) valid.GlobalCfg {
	g := valid.NewGlobalCfgFromArgs(valid.GlobalCfgArgs{})
	for _, tool := range tools {
		g.Repos = append(g.Repos, valid.Repo{IDRegex: regexp.MustCompile(".*"), AllowedOverrides: allowed, Tool: &tool})
	}
	return g
}

func TestToolDefaultsToTerraform(t *testing.T) {
	g := cfgWithTool(nil)
	log := logging.NewNoopLogger(t)
	Equals(t, valid.ToolTerraform, g.MergeProjectCfg(log, "github.com/org/repo", valid.Project{Dir: "."}, valid.RepoCfg{}).Tool)
	Equals(t, valid.ToolTerraform, g.DefaultProjCfg(log, "github.com/org/repo", ".", "default").Tool)
}

func TestToolFromServerSideRepo(t *testing.T) {
	// The last matching entry wins.
	g := cfgWithTool(nil, valid.ToolTerraform, valid.ToolTerragrunt)
	log := logging.NewNoopLogger(t)
	Equals(t, valid.ToolTerragrunt, g.MergeProjectCfg(log, "github.com/org/repo", valid.Project{Dir: "."}, valid.RepoCfg{}).Tool)
	Equals(t, valid.ToolTerragrunt, g.DefaultProjCfg(log, "github.com/org/repo", ".", "default").Tool)
}

func TestToolProjectOverrideWhenAllowed(t *testing.T) {
	tg := valid.ToolTerragrunt
	project := valid.Project{Dir: ".", Tool: &tg}
	log := logging.NewNoopLogger(t)

	allowed := cfgWithTool([]string{valid.ToolKey}, valid.ToolTerraform)
	Equals(t, valid.ToolTerragrunt, allowed.MergeProjectCfg(log, "github.com/org/repo", project, valid.RepoCfg{}).Tool)
	Ok(t, allowed.ValidateRepoCfg(valid.RepoCfg{Projects: []valid.Project{project}}, "github.com/org/repo"))

	notAllowed := cfgWithTool(nil, valid.ToolTerraform)
	Equals(t, valid.ToolTerraform, notAllowed.MergeProjectCfg(log, "github.com/org/repo", project, valid.RepoCfg{}).Tool)
	err := notAllowed.ValidateRepoCfg(valid.RepoCfg{Projects: []valid.Project{project}}, "github.com/org/repo")
	ErrContains(t, "repo config not allowed to set 'tool' key", err)
}

func TestStackIsMerged(t *testing.T) {
	g := cfgWithTool(nil, valid.ToolCdktn)
	m := g.MergeProjectCfg(logging.NewNoopLogger(t), "github.com/org/repo", valid.Project{Dir: "infra", Stack: "network"}, valid.RepoCfg{})
	Equals(t, valid.ToolCdktn, m.Tool)
	Equals(t, "network", m.Stack)
}

func TestTerraformDistributionFromServerSideRepo(t *testing.T) {
	tofu, tf := "opentofu", "terraform"
	g := valid.NewGlobalCfgFromArgs(valid.GlobalCfgArgs{})
	g.Repos = append(g.Repos, valid.Repo{IDRegex: regexp.MustCompile(".*"), TerraformDistribution: &tofu})
	log := logging.NewNoopLogger(t)

	Equals(t, &tofu, g.MergeProjectCfg(log, "github.com/org/repo", valid.Project{Dir: "."}, valid.RepoCfg{}).TerraformDistribution)
	Equals(t, &tofu, g.DefaultProjCfg(log, "github.com/org/repo", ".", "default").TerraformDistribution)
	// The project's own setting wins.
	Equals(t, &tf, g.MergeProjectCfg(log, "github.com/org/repo", valid.Project{Dir: ".", TerraformDistribution: &tf}, valid.RepoCfg{}).TerraformDistribution)
	// No server-side setting leaves the server default.
	Assert(t, valid.NewGlobalCfgFromArgs(valid.GlobalCfgArgs{}).DefaultProjCfg(log, "github.com/org/repo", ".", "default").TerraformDistribution == nil, "expected the server default")
}

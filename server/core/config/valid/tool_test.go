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

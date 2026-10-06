package events

import (
	"github.com/runatlantis/atlantis/server/core/config/valid"
	"github.com/runatlantis/atlantis/server/logging"
)

// ToolProjectDiscoverer finds the projects of a cloned repo for a tool with
// its own project layout, such as Terragrunt units or CDK Terrain stacks.
type ToolProjectDiscoverer interface {
	Discover(log logging.SimpleLogging, repoDir string) ([]valid.Project, error)
}

package events

import (
	"github.com/runatlantis/atlantis/server/core/config/valid"
	"github.com/runatlantis/atlantis/server/logging"
)

// TerragruntUnitDiscoverer finds the Terragrunt units of a cloned repo and
// returns them as projects.
type TerragruntUnitDiscoverer interface {
	Discover(log logging.SimpleLogging, repoDir string) ([]valid.Project, error)
}

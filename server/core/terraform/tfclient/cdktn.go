package tfclient

import (
	"github.com/runatlantis/atlantis/server/core/cdktn"
	"github.com/runatlantis/atlantis/server/core/config/valid"
	"github.com/runatlantis/atlantis/server/events/command"
)

// workingDir is the directory a command runs in. For a CDK Terrain project
// path is the app; the app is synthesized for the current commit if needed,
// and the command runs in the project's synthesized stack. Plan files keep
// their paths under the project directory.
func (c *DefaultClient) workingDir(ctx command.ProjectContext, path string) (string, error) {
	if ctx.Tool != valid.ToolCdktn {
		return path, nil
	}
	s := c.cdktn
	if s == nil {
		s = cdktn.Default
	}
	return s.StackDir(ctx.Log, path, ctx.Stack)
}

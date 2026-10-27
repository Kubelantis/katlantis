package cmd

import (
	"testing"

	. "github.com/runatlantis/atlantis/testing"
)

func TestTestdriveExplainsItNeedsKubernetes(t *testing.T) {
	c := (&TestdriveCmd{}).Init()
	c.SetArgs(nil)
	ErrContains(t, "cannot run without a Kubernetes API", c.Execute())
	Assert(t, c.Hidden, "testdrive should be hidden from help")
}

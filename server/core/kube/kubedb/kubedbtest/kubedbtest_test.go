package kubedbtest_test

import (
	"testing"

	"github.com/runatlantis/atlantis/server/core/db"
	"github.com/runatlantis/atlantis/server/core/db/dbtest"
	"github.com/runatlantis/atlantis/server/core/kube/kubedb/kubedbtest"
)

// The fake-client database must behave like the real backend.
func TestConformance(t *testing.T) {
	dbtest.Run(t, func(t *testing.T) db.Database { return kubedbtest.New(t) })
}

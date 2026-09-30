// Package kubedbtest provides a KubeDB backed by an in-memory Kubernetes
// client, for tests that need a real db.Database without an API server.
package kubedbtest

import (
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/runatlantis/atlantis/server/core/kube"
	"github.com/runatlantis/atlantis/server/core/kube/kubedb"
)

// FakeClient returns an empty in-memory Kubernetes client that knows the
// core and Atlantis types.
func FakeClient(tb testing.TB) client.Client {
	tb.Helper()
	scheme, err := kube.NewScheme()
	if err != nil {
		tb.Fatal(err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).Build()
}

// New returns an empty KubeDB on a fake client.
func New(tb testing.TB) *kubedb.KubeDB {
	tb.Helper()
	d, err := kubedb.New(kubedb.Config{
		Client:    FakeClient(tb),
		Namespace: "atlantis",
		Identity:  "atlantis-test",
	})
	if err != nil {
		tb.Fatal(err)
	}
	return d
}

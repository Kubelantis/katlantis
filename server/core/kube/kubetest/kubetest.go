// Package kubetest starts a real kube-apiserver and etcd (envtest) for tests
// of the Kubernetes-native backends. Tests are skipped unless
// KUBEBUILDER_ASSETS points at the control-plane binaries; run
// `make envtest` to install them.
package kubetest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/runatlantis/atlantis/server/core/kube"
)

var (
	once    sync.Once
	shared  client.Client
	initErr error
	nsSeq   atomic.Int64
)

// Client returns a client for a shared envtest API server and a fresh
// namespace for this test. The server is started once per test binary and
// left for the OS to clean up when the process exits.
func Client(t testing.TB) (client.Client, string) {
	t.Helper()
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; run `make envtest` to enable Kubernetes integration tests")
	}
	once.Do(func() {
		_, file, _, _ := runtime.Caller(0)
		root := filepath.Join(filepath.Dir(file), "..", "..", "..", "..")
		env := &envtest.Environment{
			CRDDirectoryPaths:     []string{filepath.Join(root, "deploy", "crds")},
			ErrorIfCRDPathMissing: true,
		}
		cfg, err := env.Start()
		if err != nil {
			initErr = fmt.Errorf("starting envtest: %w", err)
			return
		}
		s, err := kube.NewScheme()
		if err != nil {
			initErr = err
			return
		}
		shared, initErr = client.New(cfg, client.Options{Scheme: s})
	})
	if initErr != nil {
		t.Fatal(initErr)
	}
	ns := fmt.Sprintf("test-%d-%d", os.Getpid(), nsSeq.Add(1))
	if err := shared.Create(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}); err != nil {
		t.Fatal(err)
	}
	return shared, ns
}

package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/runatlantis/atlantis/server/core/db/mocks"
	"github.com/runatlantis/atlantis/server/events"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

// In Kubernetes mode readiness must not depend on the shared API server:
// if it did, an API outage would remove every replica from the Service.
func TestReadyzKubernetesIgnoresAPIHealth(t *testing.T) {
	database := mocks.NewMockDatabase(gomock.NewController(t))
	database.EXPECT().Ping().Return(errors.New("apiserver unreachable")).AnyTimes()
	kube := &kubeRuntime{}
	s := &Server{database: database, kube: kube, Drainer: &events.Drainer{}}

	get := func() (int, string) {
		w := httptest.NewRecorder()
		s.Readyz(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		return w.Code, w.Body.String()
	}

	code, body := get()
	assert.Equal(t, http.StatusServiceUnavailable, code, "not ready before the cluster runtime starts")
	assert.Contains(t, body, "starting")

	kube.started.Store(true)
	code, _ = get()
	assert.Equal(t, http.StatusOK, code, "ready even though the API server ping fails")

	s.Drainer.ShutdownBlocking()
	code, body = get()
	assert.Equal(t, http.StatusServiceUnavailable, code)
	assert.Contains(t, body, "draining")
}

// Outside Kubernetes mode the upstream behaviour is unchanged.
func TestReadyzSingleReplicaStillPingsDatabase(t *testing.T) {
	database := mocks.NewMockDatabase(gomock.NewController(t))
	database.EXPECT().Ping().Return(errors.New("down"))
	s := &Server{database: database}
	w := httptest.NewRecorder()
	s.Readyz(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

package cluster

import (
	"context"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/runatlantis/atlantis/server/core/kube"
	"github.com/runatlantis/atlantis/server/logging"
)

// Housekeeper runs on the leader and deletes Leases left behind by replicas
// that died without cleaning up (OOM kill, node loss). Expired leases are
// already ignored by readers; this only keeps the namespace tidy.
type Housekeeper struct {
	Client    client.Client
	Namespace string
	Logger    logging.SimpleLogging
	Interval  time.Duration
	// Grace is how long past expiry a lease is kept before deletion.
	Grace time.Duration
}

// Run deletes stale leases every Interval until ctx is done.
func (h *Housekeeper) Run(ctx context.Context) {
	if h.Interval == 0 {
		h.Interval = time.Minute
	}
	if h.Grace == 0 {
		h.Grace = 5 * time.Minute
	}
	t := time.NewTicker(h.Interval)
	defer t.Stop()
	for {
		if n, err := h.sweep(ctx); err != nil {
			h.Logger.Warn("housekeeping: %s", err)
		} else if n > 0 {
			h.Logger.Info("housekeeping: deleted %d stale leases", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (h *Housekeeper) sweep(ctx context.Context) (int, error) {
	deleted := 0
	for _, typ := range []string{kube.TypeMember, kube.TypePullLock} {
		var list coordinationv1.LeaseList
		if err := h.Client.List(ctx, &list, client.InNamespace(h.Namespace), client.MatchingLabels(kube.Labels(typ, nil))); err != nil {
			return deleted, err
		}
		cutoff := time.Now().Add(-h.Grace)
		for i := range list.Items {
			l := &list.Items[i]
			if !Expired(l, cutoff) {
				continue
			}
			uid, rv := l.UID, l.ResourceVersion
			err := h.Client.Delete(ctx, l, client.Preconditions{UID: &uid, ResourceVersion: &rv})
			if err != nil && !apierrors.IsNotFound(err) && !apierrors.IsConflict(err) {
				return deleted, err
			}
			if err == nil {
				deleted++
			}
		}
	}
	return deleted, nil
}

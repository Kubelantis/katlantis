package cluster

import (
	"context"
	"errors"
	"fmt"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ErrHeld is returned when a lease is held by another live holder.
var ErrHeld = errors.New("lease is held by another holder")

// TimedLease is a small helper around a coordination.k8s.io Lease with
// holder/expiry semantics, used for membership and pull locks. Leader
// election uses client-go's implementation instead.
type TimedLease struct {
	Client    client.Client
	Namespace string
	Name      string
	Holder    string
	Duration  time.Duration
	Labels    map[string]string
	// Annotations are written on every acquire and renew.
	Annotations map[string]string
	Now         func() time.Time
}

func (l *TimedLease) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

// Expired reports whether a lease's holder has stopped renewing it.
func Expired(lease *coordinationv1.Lease, now time.Time) bool {
	if lease.Spec.RenewTime == nil || lease.Spec.LeaseDurationSeconds == nil {
		return true
	}
	d := time.Duration(*lease.Spec.LeaseDurationSeconds) * time.Second
	return now.After(lease.Spec.RenewTime.Add(d))
}

// Holder returns the holder identity of a lease.
func Holder(lease *coordinationv1.Lease) string {
	if lease.Spec.HolderIdentity == nil {
		return ""
	}
	return *lease.Spec.HolderIdentity
}

// Acquire takes the lease if it is free, expired, or already ours, and
// renews it if ours. It returns ErrHeld (wrapped with the holder) otherwise.
func (l *TimedLease) Acquire(ctx context.Context) error {
	now := metav1.NewMicroTime(l.now())
	secs := int32(l.Duration / time.Second)
	holder := l.Holder

	var lease coordinationv1.Lease
	err := l.Client.Get(ctx, client.ObjectKey{Namespace: l.Namespace, Name: l.Name}, &lease)
	if apierrors.IsNotFound(err) {
		lease = coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{Name: l.Name, Namespace: l.Namespace, Labels: l.Labels, Annotations: l.Annotations},
			Spec: coordinationv1.LeaseSpec{
				HolderIdentity:       &holder,
				LeaseDurationSeconds: &secs,
				AcquireTime:          &now,
				RenewTime:            &now,
			},
		}
		err = l.Client.Create(ctx, &lease)
		if apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("%w: lost acquire race", ErrHeld)
		}
		return err
	}
	if err != nil {
		return err
	}

	if cur := Holder(&lease); cur != holder && !Expired(&lease, now.Time) {
		return fmt.Errorf("%w: %s", ErrHeld, cur)
	}
	if Holder(&lease) != holder {
		lease.Spec.AcquireTime = &now
		transitions := int32(0)
		if lease.Spec.LeaseTransitions != nil {
			transitions = *lease.Spec.LeaseTransitions
		}
		transitions++
		lease.Spec.LeaseTransitions = &transitions
	}
	lease.Spec.HolderIdentity = &holder
	lease.Spec.LeaseDurationSeconds = &secs
	lease.Spec.RenewTime = &now
	if lease.Labels == nil {
		lease.Labels = map[string]string{}
	}
	for k, v := range l.Labels {
		lease.Labels[k] = v
	}
	if lease.Annotations == nil {
		lease.Annotations = map[string]string{}
	}
	for k, v := range l.Annotations {
		lease.Annotations[k] = v
	}
	// Update carries the resourceVersion we read, so two replicas taking over
	// the same expired lease cannot both succeed.
	err = l.Client.Update(ctx, &lease)
	if apierrors.IsConflict(err) {
		return fmt.Errorf("%w: lost takeover race", ErrHeld)
	}
	return err
}

// Release deletes the lease if we still hold it.
func (l *TimedLease) Release(ctx context.Context) error {
	var lease coordinationv1.Lease
	err := l.Client.Get(ctx, client.ObjectKey{Namespace: l.Namespace, Name: l.Name}, &lease)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if Holder(&lease) != l.Holder {
		return nil
	}
	uid, rv := lease.UID, lease.ResourceVersion
	err = l.Client.Delete(ctx, &lease, client.Preconditions{UID: &uid, ResourceVersion: &rv})
	if apierrors.IsNotFound(err) || apierrors.IsConflict(err) {
		return nil
	}
	return err
}

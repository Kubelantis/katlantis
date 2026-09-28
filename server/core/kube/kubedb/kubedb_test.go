package kubedb_test

import (
	"context"
	"strings"
	"testing"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/runatlantis/atlantis/server/core/db"
	"github.com/runatlantis/atlantis/server/core/db/dbtest"
	"github.com/runatlantis/atlantis/server/core/kube/kubedb"
	"github.com/runatlantis/atlantis/server/core/kube/kubetest"
	"github.com/runatlantis/atlantis/server/events/models"
	. "github.com/runatlantis/atlantis/testing"
)

func newDB(t *testing.T) (*kubedb.KubeDB, client.Client, string) {
	c, ns := kubetest.Client(t)
	d, err := kubedb.New(kubedb.Config{Client: c, Namespace: ns, Identity: "atlantis-0"})
	Ok(t, err)
	return d, c, ns
}

func TestConformance(t *testing.T) {
	dbtest.Run(t, func(t *testing.T) db.Database {
		d, _, _ := newDB(t)
		return d
	})
}

func TestUnlockByPullMatchesRepoExactly(t *testing.T) {
	d, _, _ := newDB(t)
	lock := func(repo string) models.ProjectLock {
		return models.ProjectLock{
			Project:   models.NewProject(repo, ".", ""),
			Pull:      models.PullRequest{Num: 1},
			Workspace: "default",
			Time:      time.Now(),
		}
	}
	_, _, err := d.TryLock(lock("org/repo"))
	Ok(t, err)
	_, _, err = d.TryLock(lock("org/repo2"))
	Ok(t, err)

	unlocked, err := d.UnlockByPull("org/repo", 1)
	Ok(t, err)
	Equals(t, 1, len(unlocked))
	remaining, err := d.List()
	Ok(t, err)
	Equals(t, "org/repo2", remaining[0].Project.RepoFullName)
}

func TestLockDoesNotStoreCredentials(t *testing.T) {
	d, c, ns := newDB(t)
	l := models.ProjectLock{
		Project:   models.NewProject("org/repo", ".", ""),
		Pull:      models.PullRequest{Num: 1, Body: "long body", BaseRepo: models.Repo{CloneURL: "https://user:s3cret@github.com/org/repo.git"}},
		Workspace: "default",
		Time:      time.Now(),
	}
	_, _, err := d.TryLock(l)
	Ok(t, err)

	var leases coordinationv1.LeaseList
	Ok(t, c.List(context.Background(), &leases, client.InNamespace(ns)))
	Equals(t, 1, len(leases.Items))
	for k, v := range leases.Items[0].Annotations {
		Assert(t, !strings.Contains(v, "s3cret"), "annotation %s leaks credentials", k)
	}
}

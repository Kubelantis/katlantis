// Copyright 2026 The Atlantis Authors
// SPDX-License-Identifier: Apache-2.0

package events

import (
	"errors"
	"regexp"
	"testing"

	"github.com/runatlantis/atlantis/server/core/config/valid"
	"github.com/runatlantis/atlantis/server/events/command"
	"github.com/runatlantis/atlantis/server/events/models"
	"github.com/runatlantis/atlantis/server/logging"
	. "github.com/runatlantis/atlantis/testing"
)

type countingDiscoverer struct{ calls int }

func (d *countingDiscoverer) Discover(logging.SimpleLogging, string) ([]valid.Project, error) {
	d.calls++
	return []valid.Project{{Dir: "unit"}}, nil
}

func toolBuilder(planReqs []string) (*DefaultProjectCommandBuilder, *countingDiscoverer) {
	tool := valid.ToolTerragrunt
	g := valid.NewGlobalCfgFromArgs(valid.GlobalCfgArgs{})
	g.Repos = append(g.Repos, valid.Repo{
		IDRegex:          regexp.MustCompile(".*"),
		PlanRequirements: planReqs,
		Tool:             &tool,
	})
	d := &countingDiscoverer{}
	return &DefaultProjectCommandBuilder{
		GlobalCfg:    g,
		ToolProjects: map[string]ToolProjectDiscoverer{valid.ToolTerragrunt: d},
	}, d
}

func toolCtx(t *testing.T) *command.Context {
	return &command.Context{
		Log:  logging.NewNoopLogger(t),
		Pull: models.PullRequest{Num: 1, HeadCommit: "abc", BaseRepo: models.Repo{FullName: "org/repo", VCSHost: models.VCSHost{Hostname: "github.com"}}},
	}
}

func TestWithToolProjects_NoApprovalRequirementDiscovers(t *testing.T) {
	p, d := toolBuilder(nil)
	cfg, err := p.withToolProjects(toolCtx(t), t.TempDir(), valid.RepoCfg{})
	Ok(t, err)
	Equals(t, 1, d.calls)
	Equals(t, 1, len(cfg.Projects))
}

func TestWithToolProjects_UnapprovedPullDoesNotRunDiscovery(t *testing.T) {
	p, d := toolBuilder([]string{"approved"})
	ctx := toolCtx(t)
	ctx.PullRequestStatusFetched = true
	_, err := p.withToolProjects(ctx, t.TempDir(), valid.RepoCfg{})
	ErrContains(t, "needs the pull request to be approved", err)
	Equals(t, 0, d.calls)
}

func TestWithToolProjects_ApprovedPullDiscovers(t *testing.T) {
	p, d := toolBuilder([]string{"approved"})
	ctx := toolCtx(t)
	ctx.PullRequestStatusFetched = true
	ctx.PullRequestStatus.ApprovalStatus.IsApproved = true
	_, err := p.withToolProjects(ctx, t.TempDir(), valid.RepoCfg{})
	Ok(t, err)
	Equals(t, 1, d.calls)
}

type fakeStatusFetcher struct {
	status models.PullReqStatus
	err    error
	calls  int
}

func (f *fakeStatusFetcher) FetchPullStatus(logging.SimpleLogging, models.PullRequest) (models.PullReqStatus, error) {
	f.calls++
	return f.status, f.err
}

// Stored plan results do not stand in for an approval: the approval may have
// been dismissed since, and results are also stored for plans that failed.
func TestWithToolProjects_StoredResultsAtHeadDoNotCountAsApproval(t *testing.T) {
	p, d := toolBuilder([]string{"approved"})
	ctx := toolCtx(t)
	ctx.PullStatus = &models.PullStatus{
		Pull:     models.PullRequest{HeadCommit: "abc"},
		Projects: []models.ProjectStatus{{RepoRelDir: "unit", Status: models.ErroredPlanStatus}},
	}
	_, err := p.withToolProjects(ctx, t.TempDir(), valid.RepoCfg{})
	ErrContains(t, "needs the pull request to be approved", err)
	Equals(t, 0, d.calls)
}

// Commands that do not fetch the pull request status themselves, such as
// version, state rm and approve_policies, get it fetched before discovery.
func TestWithToolProjects_FetchesApprovalWhenNotFetched(t *testing.T) {
	approved := models.PullReqStatus{ApprovalStatus: models.ApprovalStatus{IsApproved: true}}
	cases := map[string]struct {
		fetcher  *fakeStatusFetcher
		wantErr  string
		discover int
	}{
		"approved":     {fetcher: &fakeStatusFetcher{status: approved}, discover: 1},
		"not approved": {fetcher: &fakeStatusFetcher{}, wantErr: "needs the pull request to be approved"},
		"fetch fails":  {fetcher: &fakeStatusFetcher{err: errors.New("vcs down")}, wantErr: "vcs down"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			p, d := toolBuilder([]string{"approved"})
			p.PullReqStatusFetcher = c.fetcher
			_, err := p.withToolProjects(toolCtx(t), t.TempDir(), valid.RepoCfg{})
			if c.wantErr != "" {
				ErrContains(t, c.wantErr, err)
			} else {
				Ok(t, err)
			}
			Equals(t, 1, c.fetcher.calls)
			Equals(t, c.discover, d.calls)
		})
	}
}

func TestWithToolProjects_DoesNotRefetchApproval(t *testing.T) {
	p, d := toolBuilder([]string{"approved"})
	f := &fakeStatusFetcher{}
	p.PullReqStatusFetcher = f
	ctx := toolCtx(t)
	ctx.PullRequestStatusFetched = true
	ctx.PullRequestStatus.ApprovalStatus.IsApproved = true
	_, err := p.withToolProjects(ctx, t.TempDir(), valid.RepoCfg{})
	Ok(t, err)
	Equals(t, 0, f.calls)
	Equals(t, 1, d.calls)
}

func TestWithToolProjects_NoApprovalRequirementDoesNotFetch(t *testing.T) {
	p, d := toolBuilder(nil)
	f := &fakeStatusFetcher{}
	p.PullReqStatusFetcher = f
	_, err := p.withToolProjects(toolCtx(t), t.TempDir(), valid.RepoCfg{})
	Ok(t, err)
	Equals(t, 0, f.calls)
	Equals(t, 1, d.calls)
}

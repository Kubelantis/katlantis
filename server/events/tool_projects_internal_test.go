// Copyright 2026 The Atlantis Authors
// SPDX-License-Identifier: Apache-2.0

package events

import (
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

func TestWithToolProjects_UnfetchedStatusUsesPlannedHead(t *testing.T) {
	p, d := toolBuilder([]string{"approved"})
	ctx := toolCtx(t)

	// Never planned: blocked.
	_, err := p.withToolProjects(ctx, t.TempDir(), valid.RepoCfg{})
	ErrContains(t, "needs the pull request to be approved", err)

	// Planned at an older head: still blocked.
	ctx.PullStatus = &models.PullStatus{Pull: models.PullRequest{HeadCommit: "old"}, Projects: []models.ProjectStatus{{RepoRelDir: "unit"}}}
	_, err = p.withToolProjects(ctx, t.TempDir(), valid.RepoCfg{})
	ErrContains(t, "needs the pull request to be approved", err)
	Equals(t, 0, d.calls)

	// Planned at this head: the plan already passed the requirement.
	ctx.PullStatus.Pull.HeadCommit = "abc"
	_, err = p.withToolProjects(ctx, t.TempDir(), valid.RepoCfg{})
	Ok(t, err)
	Equals(t, 1, d.calls)
}

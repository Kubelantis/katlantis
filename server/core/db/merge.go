package db

import (
	"github.com/runatlantis/atlantis/server/events/command"
	"github.com/runatlantis/atlantis/server/events/models"
)

// MergePullResults folds newResults into the current pull status and returns
// the status that should be persisted. It mirrors the semantics of the BoltDB
// and Redis backends so every Database implementation behaves identically:
//   - if there is no current status, or it belongs to an older head commit or
//     a different base branch, the status is rebuilt from newResults while
//     preserving any policy approvals of matching projects;
//   - otherwise matching projects are updated in place and new ones appended.
func MergePullResults(curr *models.PullStatus, pull models.PullRequest, newResults []command.ProjectResult) models.PullStatus {
	if curr == nil || PullStatusOutdated(curr.Pull, pull) {
		var statuses []models.ProjectStatus
		for _, r := range newResults {
			statuses = append(statuses, ProjectResultToStatus(r))
		}
		if curr != nil {
			for i := range statuses {
				for _, old := range curr.Projects {
					if sameProject(statuses[i], old) && len(old.PolicyStatus) > 0 {
						statuses[i].PolicyStatus = old.PolicyStatus
						break
					}
				}
			}
		}
		return models.PullStatus{Pull: pull, Projects: statuses}
	}

	newStatus := *curr
	newStatus.Projects = append([]models.ProjectStatus(nil), curr.Projects...)
	for _, res := range newResults {
		updatedExisting := false
		for i := range newStatus.Projects {
			proj := &newStatus.Projects[i]
			if res.Workspace != proj.Workspace || res.RepoRelDir != proj.RepoRelDir || res.ProjectName != proj.ProjectName {
				continue
			}
			proj.Status = res.PlanStatus()
			if len(proj.PolicyStatus) > 0 {
				for j, oldPolicySet := range proj.PolicyStatus {
					for _, newPolicySet := range res.PolicyStatus() {
						if oldPolicySet.PolicySetName == newPolicySet.PolicySetName {
							proj.PolicyStatus[j] = newPolicySet
						}
					}
				}
			} else {
				proj.PolicyStatus = res.PolicyStatus()
			}
			if risk := res.PlanRisk(); risk != nil {
				proj.PlanRisk = risk
			}
			updatedExisting = true
			break
		}
		if !updatedExisting {
			newStatus.Projects = append(newStatus.Projects, ProjectResultToStatus(res))
		}
	}
	return newStatus
}

// PullStatusOutdated reports whether a stored status belongs to a different
// revision of the pull request than pull.
func PullStatusOutdated(statusPull models.PullRequest, pull models.PullRequest) bool {
	if statusPull.HeadCommit != pull.HeadCommit {
		return true
	}
	if pull.BaseBranch == "" {
		return false
	}
	return statusPull.BaseBranch == "" || statusPull.BaseBranch != pull.BaseBranch
}

// ProjectResultToStatus converts a command result into a persisted project status.
func ProjectResultToStatus(p command.ProjectResult) models.ProjectStatus {
	return models.ProjectStatus{
		Workspace:    p.Workspace,
		RepoRelDir:   p.RepoRelDir,
		ProjectName:  p.ProjectName,
		PolicyStatus: p.PolicyStatus(),
		Status:       p.PlanStatus(),
		PlanRisk:     p.PlanRisk(),
	}
}

func sameProject(a, b models.ProjectStatus) bool {
	return a.Workspace == b.Workspace && a.RepoRelDir == b.RepoRelDir && a.ProjectName == b.ProjectName
}

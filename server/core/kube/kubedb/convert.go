package kubedb

import (
	"fmt"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/runatlantis/atlantis/server/core/kube/apis/v1alpha1"
	"github.com/runatlantis/atlantis/server/events/models"
)

var planStatusByName = map[string]models.ProjectPlanStatus{}

func init() {
	for s := models.ErroredPlanStatus; s <= models.PassedPolicyCheckStatus; s++ {
		planStatusByName[s.String()] = s
	}
}

func pullStatusToSpec(s models.PullStatus) v1alpha1.PullStatusSpec {
	spec := v1alpha1.PullStatusSpec{
		Pull:         pullToAPI(s.Pull),
		ProjectCount: len(s.Projects),
	}
	for _, p := range s.Projects {
		spec.Projects = append(spec.Projects, projectToAPI(p))
	}
	return spec
}

func specToPullStatus(spec v1alpha1.PullStatusSpec) (models.PullStatus, error) {
	s := models.PullStatus{Pull: pullFromAPI(spec.Pull)}
	for _, p := range spec.Projects {
		mp, err := projectFromAPI(p)
		if err != nil {
			return models.PullStatus{}, err
		}
		s.Projects = append(s.Projects, mp)
	}
	return s, nil
}

func pullToAPI(p models.PullRequest) v1alpha1.PullRequest {
	state := "open"
	if p.State == models.ClosedPullState {
		state = "closed"
	}
	return v1alpha1.PullRequest{
		Num:                      p.Num,
		HeadCommit:               p.HeadCommit,
		URL:                      p.URL,
		HeadBranch:               p.HeadBranch,
		BaseBranch:               p.BaseBranch,
		HardenedNonPRRefCheckout: p.HardenedNonPRRefCheckout,
		Author:                   p.Author,
		State:                    state,
		BaseRepo: v1alpha1.Repo{
			FullName:          p.BaseRepo.FullName,
			Owner:             p.BaseRepo.Owner,
			Name:              p.BaseRepo.Name,
			SanitizedCloneURL: p.BaseRepo.SanitizedCloneURL,
			VCSHostname:       p.BaseRepo.VCSHost.Hostname,
			VCSHostType:       int(p.BaseRepo.VCSHost.Type),
		},
	}
}

func pullFromAPI(p v1alpha1.PullRequest) models.PullRequest {
	state := models.OpenPullState
	if p.State == "closed" {
		state = models.ClosedPullState
	}
	return models.PullRequest{
		Num:                      p.Num,
		HeadCommit:               p.HeadCommit,
		URL:                      p.URL,
		HeadBranch:               p.HeadBranch,
		BaseBranch:               p.BaseBranch,
		HardenedNonPRRefCheckout: p.HardenedNonPRRefCheckout,
		Author:                   p.Author,
		State:                    state,
		BaseRepo: models.Repo{
			FullName:          p.BaseRepo.FullName,
			Owner:             p.BaseRepo.Owner,
			Name:              p.BaseRepo.Name,
			SanitizedCloneURL: p.BaseRepo.SanitizedCloneURL,
			VCSHost: models.VCSHost{
				Hostname: p.BaseRepo.VCSHostname,
				Type:     models.VCSHostType(p.BaseRepo.VCSHostType),
			},
		},
	}
}

func projectToAPI(p models.ProjectStatus) v1alpha1.ProjectStatus {
	out := v1alpha1.ProjectStatus{
		Workspace:   p.Workspace,
		RepoRelDir:  p.RepoRelDir,
		ProjectName: p.ProjectName,
		Status:      p.Status.String(),
		PlanRisk:    riskToAPI(p.PlanRisk),
	}
	for _, ps := range p.PolicyStatus {
		aps := v1alpha1.PolicySetStatus{
			PolicySetName:   ps.PolicySetName,
			Passed:          ps.Passed,
			Hashes:          ps.Hashes,
			PolicyItemRegex: ps.PolicyItemRegex,
		}
		for _, a := range ps.Approvals {
			aps.Approvals = append(aps.Approvals, v1alpha1.PolicySetApproval{Approver: a.Approver, Hashes: a.Hashes})
		}
		out.PolicyStatus = append(out.PolicyStatus, aps)
	}
	return out
}

func projectFromAPI(p v1alpha1.ProjectStatus) (models.ProjectStatus, error) {
	status, ok := planStatusByName[p.Status]
	if !ok {
		return models.ProjectStatus{}, fmt.Errorf("unknown project status %q", p.Status)
	}
	risk, err := riskFromAPI(p.PlanRisk)
	if err != nil {
		return models.ProjectStatus{}, err
	}
	out := models.ProjectStatus{
		Workspace:   p.Workspace,
		RepoRelDir:  p.RepoRelDir,
		ProjectName: p.ProjectName,
		Status:      status,
		PlanRisk:    risk,
	}
	for _, ps := range p.PolicyStatus {
		mps := models.PolicySetStatus{
			PolicySetName:   ps.PolicySetName,
			Passed:          ps.Passed,
			Hashes:          ps.Hashes,
			PolicyItemRegex: ps.PolicyItemRegex,
		}
		for _, a := range ps.Approvals {
			mps.Approvals = append(mps.Approvals, models.PolicySetApproval{Approver: a.Approver, Hashes: a.Hashes})
		}
		out.PolicyStatus = append(out.PolicyStatus, mps)
	}
	return out, nil
}

func riskToAPI(r *models.PlanRisk) *v1alpha1.PlanRisk {
	if r == nil {
		return nil
	}
	out := &v1alpha1.PlanRisk{
		Tier:        string(r.Tier),
		Creates:     r.Creates,
		Updates:     r.Updates,
		Deletes:     r.Deletes,
		Replaces:    r.Replaces,
		BlastRadius: strconv.FormatFloat(r.BlastRadius, 'f', 3, 64),
		Model:       r.Model,
		Error:       r.Error,
		AssessedAt:  metav1.NewTime(r.AssessedAt),
	}
	for _, f := range r.Findings {
		af := v1alpha1.PlanRiskFinding{Address: f.Address, Action: f.Action, Reason: f.Reason}
		if f.Probability != 0 {
			af.Probability = strconv.FormatFloat(f.Probability, 'f', 3, 64)
		}
		out.Findings = append(out.Findings, af)
	}
	return out
}

func riskFromAPI(r *v1alpha1.PlanRisk) (*models.PlanRisk, error) {
	if r == nil {
		return nil, nil
	}
	out := &models.PlanRisk{
		Tier:       models.PlanRiskTier(r.Tier),
		Creates:    r.Creates,
		Updates:    r.Updates,
		Deletes:    r.Deletes,
		Replaces:   r.Replaces,
		Model:      r.Model,
		Error:      r.Error,
		AssessedAt: r.AssessedAt.Time,
	}
	var err error
	if r.BlastRadius != "" {
		if out.BlastRadius, err = strconv.ParseFloat(r.BlastRadius, 64); err != nil {
			return nil, fmt.Errorf("parsing blast radius %q: %w", r.BlastRadius, err)
		}
	}
	for _, f := range r.Findings {
		mf := models.PlanRiskFinding{Address: f.Address, Action: f.Action, Reason: f.Reason}
		if f.Probability != "" {
			if mf.Probability, err = strconv.ParseFloat(f.Probability, 64); err != nil {
				return nil, fmt.Errorf("parsing finding probability %q: %w", f.Probability, err)
			}
		}
		out.Findings = append(out.Findings, mf)
	}
	return out, nil
}

// sanitizeLock removes credentials before a lock is written to the API server,
// where it is readable by anyone allowed to get Leases in the namespace.
func sanitizeLock(l models.ProjectLock) models.ProjectLock {
	l.Pull.BaseRepo.CloneURL = ""
	l.Pull.Body = ""
	return l
}

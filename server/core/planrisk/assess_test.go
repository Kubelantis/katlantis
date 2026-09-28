package planrisk_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/runatlantis/atlantis/server/core/planrisk"
	"github.com/runatlantis/atlantis/server/events/models"
	. "github.com/runatlantis/atlantis/testing"
)

type rc struct {
	addr, typ string
	actions   []string
}

func showJSON(changes ...rc) []byte {
	type change struct {
		Actions []string       `json:"actions"`
		After   map[string]any `json:"after"`
	}
	var out struct {
		ResourceChanges []map[string]any `json:"resource_changes"`
	}
	for _, c := range changes {
		out.ResourceChanges = append(out.ResourceChanges, map[string]any{
			"address": c.addr, "mode": "managed", "type": c.typ,
			"change": change{Actions: c.actions, After: map[string]any{"password": "hunter2"}},
		})
	}
	out.ResourceChanges = append(out.ResourceChanges, map[string]any{
		"address": "data.aws_ami.x", "mode": "data", "type": "aws_ami", "change": change{Actions: []string{"read"}},
	})
	b, _ := json.Marshal(out)
	return b
}

// fakeEval answers every question from a function of its id.
type fakeEval struct {
	calls   int
	state   string
	answers func(id string) planrisk.Answer
	err     error
}

func (f *fakeEval) Evaluate(_ context.Context, state any, qs map[string]planrisk.Question) (*planrisk.Response, error) {
	f.calls++
	b, _ := json.Marshal(state)
	f.state = string(b)
	if f.err != nil {
		return nil, f.err
	}
	resp := &planrisk.Response{Model: "jev-1.13.0", Answers: map[string]planrisk.Answer{}}
	for id := range qs {
		resp.Answers[id] = f.answers(id)
	}
	return resp, nil
}

// judge returns answers: noul for ids in yes, 0.05 otherwise; blast score b.
func judge(b float64, yes ...string) func(string) planrisk.Answer {
	return func(id string) planrisk.Answer {
		if id == "blast_radius" {
			return planrisk.Answer{Type: "score", Score: b}
		}
		for _, y := range yes {
			if y == id {
				return planrisk.Answer{Type: "noul", Noul: 0.92}
			}
		}
		return planrisk.Answer{Type: "noul", Noul: 0.05}
	}
}

var proj = planrisk.Project{Repository: "org/infra", Directory: "envs/dev", Workspace: "default"}

func assess(t *testing.T, ev planrisk.Evaluator, plan []byte) *models.PlanRisk {
	a := &planrisk.Assessor{Evaluator: ev, FailureTier: models.PlanRiskHigh, MaxChanges: 3}
	return a.Assess(context.Background(), proj, plan)
}

func TestNoChangesSkipsModel(t *testing.T) {
	ev := &fakeEval{}
	r := assess(t, ev, showJSON())
	Equals(t, models.PlanRiskLow, r.Tier)
	Equals(t, 0, ev.calls)
}

func TestTiers(t *testing.T) {
	create := rc{"aws_s3_bucket.logs", "aws_s3_bucket", []string{"create"}}
	update := rc{"aws_instance.web", "aws_instance", []string{"update"}}
	delSG := rc{"aws_security_group_rule.old", "aws_security_group_rule", []string{"delete"}}
	replaceDB := rc{"aws_db_instance.main", "aws_db_instance", []string{"delete", "create"}}

	cases := []struct {
		name   string
		plan   []rc
		judge  func(string) planrisk.Answer
		want   models.PlanRiskTier
		counts [4]int
	}{
		{"additive dev change", []rc{create}, judge(0.2), models.PlanRiskLow, [4]int{1, 0, 0, 0}},
		{"any delete is at least medium", []rc{delSG}, judge(0.1), models.PlanRiskMedium, [4]int{0, 0, 1, 0}},
		{"security change is medium", []rc{create}, judge(0.2, "security_0"), models.PlanRiskMedium, [4]int{1, 0, 0, 0}},
		{"prod update is medium", []rc{update}, judge(0.2, "production"), models.PlanRiskMedium, [4]int{0, 1, 0, 0}},
		{"broad blast radius is high", []rc{update}, judge(2.0), models.PlanRiskHigh, [4]int{0, 1, 0, 0}},
		{"security change in prod is high", []rc{create}, judge(0.2, "security_0", "production"), models.PlanRiskHigh, [4]int{1, 0, 0, 0}},
		// replaceDB sorts first, so it is changes[0].
		{"data loss is high", []rc{create, replaceDB}, judge(0.5, "data_loss_0"), models.PlanRiskHigh, [4]int{1, 0, 0, 1}},
		{"data loss in prod is critical", []rc{create, replaceDB}, judge(0.5, "data_loss_0", "production"), models.PlanRiskCritical, [4]int{1, 0, 0, 1}},
		{"critical blast radius", []rc{update}, judge(2.8), models.PlanRiskCritical, [4]int{0, 1, 0, 0}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev := &fakeEval{answers: c.judge}
			r := assess(t, ev, showJSON(c.plan...))
			Equals(t, c.want, r.Tier)
			Equals(t, c.counts, [4]int{r.Creates, r.Updates, r.Deletes, r.Replaces})
			Equals(t, "jev-1.13.0", r.Model)
			Equals(t, "", r.Error)
		})
	}
}

func TestFindingsNameTheResource(t *testing.T) {
	ev := &fakeEval{answers: judge(0.5, "data_loss_0")}
	r := assess(t, ev, showJSON(rc{"aws_db_instance.main", "aws_db_instance", []string{"delete"}}))
	Equals(t, 1, len(r.Findings))
	Equals(t, "aws_db_instance.main", r.Findings[0].Address)
	Equals(t, "delete", r.Findings[0].Action)
	Equals(t, 0.92, r.Findings[0].Probability)
}

func TestNeverSendsAttributeValues(t *testing.T) {
	ev := &fakeEval{answers: judge(0.1)}
	assess(t, ev, showJSON(rc{"aws_db_instance.main", "aws_db_instance", []string{"create"}}))
	Assert(t, !strings.Contains(ev.state, "hunter2"), "state leaked an attribute value: %s", ev.state)
	Assert(t, !strings.Contains(ev.state, "data.aws_ami"), "data sources must be dropped")
}

func TestUnassessedDestructiveChangesAreHigh(t *testing.T) {
	var plan []rc
	for i := range 3 {
		plan = append(plan, rc{fmt.Sprintf("aws_iam_role.r%d", i), "aws_iam_role", []string{"delete"}})
	}
	plan = append(plan, rc{"aws_s3_bucket.b", "aws_s3_bucket", []string{"delete"}})
	r := assess(t, &fakeEval{answers: judge(0.1)}, showJSON(plan...))
	Equals(t, models.PlanRiskHigh, r.Tier)
	Equals(t, 4, r.Deletes)
}

func TestFailuresUseFailureTier(t *testing.T) {
	plan := showJSON(rc{"aws_instance.web", "aws_instance", []string{"update"}})
	r := assess(t, &fakeEval{err: errors.New("boom")}, plan)
	Equals(t, models.PlanRiskHigh, r.Tier)
	ErrContains(t, "boom", errors.New(r.Error))

	missing := &fakeEval{answers: judge(0.1)}
	missingAssessor := &planrisk.Assessor{Evaluator: evalDropping{missing, "blast_radius"}, FailureTier: models.PlanRiskCritical}
	r = missingAssessor.Assess(context.Background(), proj, plan)
	Equals(t, models.PlanRiskCritical, r.Tier)

	r = assess(t, &fakeEval{}, []byte("not json"))
	Equals(t, models.PlanRiskHigh, r.Tier)
}

type evalDropping struct {
	inner planrisk.Evaluator
	drop  string
}

func (e evalDropping) Evaluate(ctx context.Context, s any, qs map[string]planrisk.Question) (*planrisk.Response, error) {
	r, err := e.inner.Evaluate(ctx, s, qs)
	if r != nil {
		delete(r.Answers, e.drop)
	}
	return r, err
}

func TestTypeSafeClientRetriesRateLimits(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Equals(t, "Bearer k", r.Header.Get("Authorization"))
		Equals(t, "/v1/systemone", r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		Assert(t, strings.Contains(string(body), `"model":"jev-1.13.0"`), "model not sent")
		if n.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, `{"model":"jev-1.13.0","answers":{"q":{"type":"noul","noul":0.7}},"usage":{"input_tokens":10,"output_tokens":1}}`)
	}))
	defer srv.Close()
	c := &planrisk.TypeSafeClient{BaseURL: srv.URL, APIKey: "k", Model: "jev-1.13.0"}
	resp, err := c.Evaluate(context.Background(), "s", map[string]planrisk.Question{"q": {Type: "noul", Instructions: "?"}})
	Ok(t, err)
	Equals(t, 0.7, resp.Answers["q"].Noul)
	Equals(t, int32(2), n.Load())
}

func TestTypeSafeClientDoesNotRetryClientErrors(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n.Add(1)
		http.Error(w, "bad question", http.StatusBadRequest)
	}))
	defer srv.Close()
	c := &planrisk.TypeSafeClient{BaseURL: srv.URL, APIKey: "k", Model: "m"}
	_, err := c.Evaluate(context.Background(), "s", nil)
	ErrContains(t, "bad question", err)
	Equals(t, int32(1), n.Load())
}

// TestLiveJev runs the real model when TYPESAFE_API_KEY is set. It checks the
// judgments on unambiguous cases, not exact probabilities.
func TestLiveJev(t *testing.T) {
	key := os.Getenv("TYPESAFE_API_KEY")
	if key == "" {
		t.Skip("TYPESAFE_API_KEY not set")
	}
	a := &planrisk.Assessor{
		Evaluator:   &planrisk.TypeSafeClient{BaseURL: "https://api.typesafe.ai", APIKey: key, Model: "jev-1.13.0"},
		FailureTier: models.PlanRiskUnknown,
		Timeout:     time.Minute,
	}
	prodDB := a.Assess(context.Background(), planrisk.Project{Repository: "acme/infra", Directory: "envs/production/payments", Workspace: "prod"},
		showJSON(rc{"aws_db_instance.payments", "aws_db_instance", []string{"delete"}}))
	t.Logf("prod db delete: %+v", prodDB)
	Equals(t, "", prodDB.Error)
	Assert(t, prodDB.Tier.Rank() >= models.PlanRiskHigh.Rank(), "deleting a production database must be at least high, got %s", prodDB.Tier)

	devTag := a.Assess(context.Background(), planrisk.Project{Repository: "acme/infra", Directory: "envs/dev/sandbox", Workspace: "dev"},
		showJSON(rc{"aws_cloudwatch_log_group.debug", "aws_cloudwatch_log_group", []string{"create"}}))
	t.Logf("dev log group create: %+v", devTag)
	Equals(t, "", devTag.Error)
	Equals(t, models.PlanRiskLow, devTag.Tier)
}

package migrate_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/runatlantis/atlantis/server/core/migrate"
	"github.com/runatlantis/atlantis/server/core/typesafe"
	. "github.com/runatlantis/atlantis/testing"
)

// TestLiveJevLabels checks the unambiguous labels with the real model. It
// runs only when TYPESAFE_API_KEY is set.
func TestLiveJevLabels(t *testing.T) {
	key := os.Getenv("TYPESAFE_API_KEY")
	if key == "" {
		t.Skip("TYPESAFE_API_KEY not set")
	}
	src := `workflows:
  w:
    plan:
      steps:
      - run: npm i && cdktf get && cdktf synth --output ci-cdktf.out
      - run: aws sts assume-role --role-arn arn:aws:iam::123:role/deployer > /tmp/creds.json
      - run: terragrunt plan -input=false -out $PLANFILE
    policy_check:
      steps:
      - show
      - run: checkov -f $SHOWFILE --compact
`
	r, err := migrate.MigrateServerConfig("repos.yaml", []byte(src))
	Ok(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	l := &migrate.JevLabeler{Evaluator: &typesafe.Client{BaseURL: "https://api.typesafe.ai", APIKey: key, Model: "jev-1.13.0"}, MinConfidence: 0.8}
	Ok(t, l.Label(ctx, r.Notes))
	want := map[string]string{
		"npm i && cdktf get && cdktf synth --output ci-cdktf.out":                         "cdktf",
		"aws sts assume-role --role-arn arn:aws:iam::123:role/deployer > /tmp/creds.json": "credentials",
		"terragrunt plan -input=false -out $PLANFILE":                                     "terragrunt",
		"checkov -f $SHOWFILE --compact":                                                  "policy_scanner",
	}
	for _, n := range r.Notes {
		if exp, ok := want[n.Command]; ok {
			Equals(t, exp, n.Label)
			delete(want, n.Command)
		}
	}
	Equals(t, 0, len(want))
}

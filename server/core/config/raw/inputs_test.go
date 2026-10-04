package raw_test

import (
	"testing"

	"github.com/runatlantis/atlantis/server/core/config/raw"
	. "github.com/runatlantis/atlantis/testing"
)

func TestInputsValidation(t *testing.T) {
	cases := []struct {
		name string
		in   raw.Inputs
		err  string
	}{
		{"valid", raw.Inputs{VarFiles: []string{"env/prod.tfvars"}, BackendConfig: []string{"b.hcl", "bucket=my-state"},
			Vars: map[string]string{"region": "x"}, Env: map[string]string{"TAG": "${BASE_REPO_NAME}-${WORKSPACE}"},
			ExtraArgs: map[string][]string{"plan": {"-lock=false"}}}, ""},
		{"absolute var file", raw.Inputs{VarFiles: []string{"/etc/secrets.tfvars"}}, "must be relative"},
		{"escaping var file", raw.Inputs{VarFiles: []string{"../other/x.tfvars"}}, "must not point outside"},
		{"escaping backend file", raw.Inputs{BackendConfig: []string{"../../b.hcl"}}, "must not point outside"},
		{"bad var name", raw.Inputs{Vars: map[string]string{"bad-name": "x"}}, "not a valid variable name"},
		{"bad env name", raw.Inputs{Env: map[string]string{"1X": "x"}}, "not a valid environment variable name"},
		{"unknown context var", raw.Inputs{Env: map[string]string{"X": "${SECRET_TOKEN}"}}, "unknown variable ${SECRET_TOKEN}"},
		{"extra args for unknown step", raw.Inputs{ExtraArgs: map[string][]string{"run": {"x"}}}, `"run" is not a built-in step`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.in.Validate()
			if c.err == "" {
				Ok(t, err)
			} else {
				ErrContains(t, c.err, err)
			}
		})
	}
}

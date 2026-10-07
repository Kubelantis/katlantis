package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/runatlantis/atlantis/server/core/config"
	"github.com/runatlantis/atlantis/server/core/config/valid"
	. "github.com/runatlantis/atlantis/testing"
)

// Keys that native workflows replaced fail with an error that names the key
// and points to atlantis migrate-workflows, instead of an unknown-field error.
func TestParseGlobalCfg_RemovedKeys(t *testing.T) {
	cases := map[string]struct {
		input  string
		expErr string
	}{
		"workflows": {
			input:  "workflows:\n  custom:\n    plan:\n      steps: [init, plan]\n",
			expErr: "workflows was removed",
		},
		"repo workflow": {
			input:  "repos:\n- id: /.*/\n  workflow: custom\n",
			expErr: "workflow was removed",
		},
		"allowed_workflows": {
			input:  "repos:\n- id: /.*/\n  allowed_workflows: [custom]\n",
			expErr: "allowed_workflows was removed",
		},
		"allow_custom_workflows": {
			input:  "repos:\n- id: /.*/\n  allow_custom_workflows: true\n",
			expErr: "allow_custom_workflows was removed",
		},
		"custom_policy_check": {
			input:  "repos:\n- id: /.*/\n  custom_policy_check: true\n",
			expErr: "custom_policy_check was removed",
		},
		"allowed_overrides workflow": {
			input:  "repos:\n- id: /.*/\n  allowed_overrides: [workflow]\n",
			expErr: `"workflow" can no longer be overridden`,
		},
		"allowed_overrides custom_policy_check": {
			input:  "repos:\n- id: /.*/\n  allowed_overrides: [custom_policy_check]\n",
			expErr: `"custom_policy_check" can no longer be overridden`,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "repos.yaml")
			Ok(t, os.WriteFile(path, []byte(c.input), 0600))
			_, err := (&config.ParserValidator{}).ParseGlobalCfg(path, valid.NewGlobalCfgFromArgs(valid.GlobalCfgArgs{}))
			ErrContains(t, c.expErr, err)
			ErrContains(t, "atlantis migrate-workflows", err)
		})
	}
}

func TestParseGlobalCfgJSON_RemovedKeys(t *testing.T) {
	_, err := (&config.ParserValidator{}).ParseGlobalCfgJSON(`{"repos": [{"id": "/.*/", "workflow": "custom"}]}`, valid.NewGlobalCfgFromArgs(valid.GlobalCfgArgs{}))
	ErrContains(t, "workflow was removed", err)
	ErrContains(t, "atlantis migrate-workflows", err)
}

func TestParseRepoCfg_RemovedKeys(t *testing.T) {
	cases := map[string]struct {
		input  string
		expErr string
	}{
		"workflows": {
			input:  "version: 3\nworkflows:\n  custom:\n    plan:\n      steps: [init, plan]\n",
			expErr: "workflows was removed",
		},
		"project workflow": {
			input:  "version: 3\nprojects:\n- dir: .\n  workflow: custom\n",
			expErr: "workflow was removed",
		},
		"project custom_policy_check": {
			input:  "version: 3\nprojects:\n- dir: .\n  custom_policy_check: true\n",
			expErr: "custom_policy_check was removed",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			Ok(t, os.WriteFile(filepath.Join(dir, "atlantis.yaml"), []byte(c.input), 0600))
			globalCfg := valid.NewGlobalCfgFromArgs(valid.GlobalCfgArgs{AllowAllRepoSettings: true})
			_, err := (&config.ParserValidator{}).ParseRepoCfg(dir, globalCfg, "github.com/owner/repo", "main")
			ErrContains(t, c.expErr, err)
			ErrContains(t, "atlantis migrate-workflows", err)
		})
	}
}

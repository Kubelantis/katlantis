package raw

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/runatlantis/atlantis/server/core/config/valid"
)

// Inputs is the raw schema of the native inputs block, accepted in
// server-side repo config entries and, when allowed_overrides includes
// "inputs", in repo-level project config.
type Inputs struct {
	VarFiles      []string            `yaml:"var_files,omitempty" json:"var_files,omitempty"`
	Vars          map[string]string   `yaml:"vars,omitempty" json:"vars,omitempty"`
	BackendConfig []string            `yaml:"backend_config,omitempty" json:"backend_config,omitempty"`
	Env           map[string]string   `yaml:"env,omitempty" json:"env,omitempty"`
	ExtraArgs     map[string][]string `yaml:"extra_args,omitempty" json:"extra_args,omitempty"`
}

var (
	identifierRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	contextRefRe = regexp.MustCompile(`\$\{([^}]*)\}`)
)

// Validate validates the inputs.
func (i *Inputs) Validate() error {
	if i == nil {
		return nil
	}
	for _, f := range i.VarFiles {
		if err := validateRelPath("var_files", f); err != nil {
			return err
		}
	}
	for _, b := range i.BackendConfig {
		// key=value pairs are passed through; anything else is a file.
		if strings.Contains(b, "=") {
			continue
		}
		if err := validateRelPath("backend_config", b); err != nil {
			return err
		}
	}
	for k := range i.Vars {
		if !identifierRe.MatchString(k) {
			return fmt.Errorf("inputs.vars: %q is not a valid variable name", k)
		}
	}
	for k, v := range i.Env {
		if !identifierRe.MatchString(k) {
			return fmt.Errorf("inputs.env: %q is not a valid environment variable name", k)
		}
		for _, m := range contextRefRe.FindAllStringSubmatch(v, -1) {
			if !slices.Contains(valid.InputsContextVars, m[1]) {
				return fmt.Errorf("inputs.env.%s: unknown variable ${%s}; available: %s", k, m[1], strings.Join(valid.InputsContextVars, ", "))
			}
		}
	}
	for step := range i.ExtraArgs {
		if !slices.Contains(valid.InputsExtraArgsSteps, step) {
			return fmt.Errorf("inputs.extra_args: %q is not a built-in step; use one of %s", step, strings.Join(valid.InputsExtraArgsSteps, ", "))
		}
	}
	return nil
}

func validateRelPath(field, p string) error {
	if p == "" {
		return fmt.Errorf("inputs.%s: empty path", field)
	}
	if filepath.IsAbs(p) {
		return fmt.Errorf("inputs.%s: %q must be relative to the project directory", field, p)
	}
	if clean := filepath.Clean(p); clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return errors.New("inputs." + field + ": " + p + " must not point outside the repository")
	}
	return nil
}

// ToValid converts to the validated form. A nil receiver returns nil.
func (i *Inputs) ToValid() *valid.Inputs {
	if i == nil {
		return nil
	}
	return &valid.Inputs{
		VarFiles:      i.VarFiles,
		Vars:          i.Vars,
		BackendConfig: i.BackendConfig,
		Env:           i.Env,
		ExtraArgs:     i.ExtraArgs,
	}
}

// toolValid validates a *string tool value.
func toolValid(value any) error {
	t, _ := value.(*string)
	if t == nil || slices.Contains(valid.Tools, *t) {
		return nil
	}
	return fmt.Errorf("%q is not a supported tool; use one of %s", *t, strings.Join(valid.Tools, ", "))
}

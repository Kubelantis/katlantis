package valid

import (
	"maps"
	"slices"
	"strings"
)

// InputsKey is the allowed_overrides key that lets repo config set inputs.
const InputsKey = "inputs"

// Inputs configures the built-in steps declaratively: variable files and
// values, backend configuration, extra arguments and environment variables.
// It replaces what custom workflows used `extra_args`, `env` and `run`
// steps for. Inputs never add steps; they only shape built-in ones.
type Inputs struct {
	// VarFiles are passed to plan and import as -var-file, in order.
	VarFiles []string
	// Vars are passed to plan and import as -var, sorted by name.
	Vars map[string]string
	// BackendConfig entries (files or key=value) are passed to init as
	// -backend-config. Init then also gets -reconfigure, so projects that
	// switch backends between environments need no cleanup step.
	BackendConfig []string
	// Env is set for every step. Values may reference ${BASE_REPO_OWNER},
	// ${BASE_REPO_NAME}, ${REPO_REL_DIR}, ${WORKSPACE}, ${PROJECT_NAME},
	// ${PULL_NUM} and ${HEAD_COMMIT}.
	Env map[string]string
	// ExtraArgs are appended to the built-in step of the same name.
	ExtraArgs map[string][]string
}

// InputsExtraArgsSteps are the built-in steps extra_args may target.
var InputsExtraArgsSteps = []string{"init", "plan", "apply", "show", "policy_check", "import", "state_rm"}

// Override returns base with every field that is set in o replacing the
// corresponding field of base. A nil o returns base unchanged.
func (base Inputs) Override(o *Inputs) Inputs {
	if o == nil {
		return base
	}
	out := base
	if o.VarFiles != nil {
		out.VarFiles = o.VarFiles
	}
	if o.Vars != nil {
		out.Vars = o.Vars
	}
	if o.BackendConfig != nil {
		out.BackendConfig = o.BackendConfig
	}
	if o.Env != nil {
		out.Env = o.Env
	}
	if o.ExtraArgs != nil {
		out.ExtraArgs = o.ExtraArgs
	}
	return out
}

// IsZero reports whether no input is configured.
func (i Inputs) IsZero() bool {
	return len(i.VarFiles) == 0 && len(i.Vars) == 0 && len(i.BackendConfig) == 0 && len(i.Env) == 0 && len(i.ExtraArgs) == 0
}

// args returns the arguments inputs add to the built-in step stepName.
func (i Inputs) args(stepName string) []string {
	var args []string
	switch stepName {
	case "init":
		if len(i.BackendConfig) > 0 && !slices.Contains(i.ExtraArgs["init"], "-reconfigure") {
			args = append(args, "-reconfigure")
		}
		for _, b := range i.BackendConfig {
			args = append(args, "-backend-config="+b)
		}
	case "plan", "import":
		for _, f := range i.VarFiles {
			args = append(args, "-var-file="+f)
		}
		for _, k := range slices.Sorted(maps.Keys(i.Vars)) {
			args = append(args, "-var", k+"="+i.Vars[k])
		}
	}
	return append(args, i.ExtraArgs[stepName]...)
}

// Apply returns a copy of w whose built-in steps carry the inputs'
// arguments. Steps that are not built in are left unchanged.
func (i Inputs) Apply(w Workflow) Workflow {
	if i.IsZero() {
		return w
	}
	apply := func(s Stage) Stage {
		steps := make([]Step, len(s.Steps))
		for n, step := range s.Steps {
			step.ExtraArgs = append(i.args(step.StepName), step.ExtraArgs...)
			if len(step.ExtraArgs) == 0 {
				step.ExtraArgs = nil
			}
			steps[n] = step
		}
		return Stage{Steps: steps}
	}
	w.Plan = apply(w.Plan)
	w.Apply = apply(w.Apply)
	w.PolicyCheck = apply(w.PolicyCheck)
	w.Import = apply(w.Import)
	w.StateRm = apply(w.StateRm)
	return w
}

// InputsContextVars are the names env values may reference.
var InputsContextVars = []string{"BASE_REPO_OWNER", "BASE_REPO_NAME", "REPO_REL_DIR", "WORKSPACE", "PROJECT_NAME", "PULL_NUM", "HEAD_COMMIT"}

// ExpandEnv resolves ${NAME} references to context values in Env.
// Unknown names are left as they are; config validation rejects them.
func (i Inputs) ExpandEnv(context map[string]string) map[string]string {
	if len(i.Env) == 0 {
		return nil
	}
	out := make(map[string]string, len(i.Env))
	for k, v := range i.Env {
		out[k] = expandContext(v, context)
	}
	return out
}

func expandContext(s string, context map[string]string) string {
	var b strings.Builder
	for {
		start := strings.Index(s, "${")
		if start < 0 {
			b.WriteString(s)
			return b.String()
		}
		end := strings.Index(s[start:], "}")
		if end < 0 {
			b.WriteString(s)
			return b.String()
		}
		name := s[start+2 : start+end]
		b.WriteString(s[:start])
		if v, ok := context[name]; ok {
			b.WriteString(v)
		} else {
			b.WriteString(s[start : start+end+1])
		}
		s = s[start+end+1:]
	}
}

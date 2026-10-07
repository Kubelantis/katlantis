// Package migrate converts Atlantis custom workflows and custom policy
// checks into katlantis native configuration (inputs). Workflow hooks are
// kept.
//
// Conversion is deterministic code. Built-in steps and their arguments,
// fixed and templated env values, and known no-op commands are converted or
// dropped by rules. Everything else is removed and listed in the report; an
// optional classifier (Jev) only labels those removed commands so the report
// can say what replaces them. It never changes the converted config.
package migrate

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Stages are the workflow stages, in the order Atlantis defines them.
var Stages = []string{"plan", "apply", "policy_check", "import", "state_rm"}

// nativeStageSteps are the built-in steps a native workflow runs per stage.
var nativeStageSteps = map[string][]string{
	"plan":         {"init", "plan"},
	"apply":        {"apply"},
	"policy_check": {"show", "policy_check"},
	"import":       {"init", "import"},
	"state_rm":     {"init", "state_rm"},
}

var builtinSteps = []string{"init", "plan", "apply", "show", "policy_check", "import", "state_rm"}

// Step is one parsed workflow step.
type Step struct {
	Stage string
	// Kind is a built-in step name, or "run", "env" or "multienv".
	Kind       string
	ExtraArgs  []string
	Command    string // run, multienv, or env with command
	EnvName    string
	EnvValue   string
	HasEnvVal  bool
	Unparsable string // set when the step could not be read
}

// parseWorkflow reads the stages of a workflow mapping node.
func parseWorkflow(wf *yaml.Node) []Step {
	var steps []Step
	for _, stage := range Stages {
		st := mapGet(wf, stage)
		if st == nil {
			continue
		}
		list := mapGet(st, "steps")
		if list == nil || list.Kind != yaml.SequenceNode {
			continue
		}
		for _, n := range list.Content {
			steps = append(steps, parseStep(stage, n))
		}
	}
	return steps
}

func parseStep(stage string, n *yaml.Node) Step {
	s := Step{Stage: stage}
	switch n.Kind {
	case yaml.ScalarNode:
		s.Kind = n.Value
		return s
	case yaml.MappingNode:
		if len(n.Content) != 2 {
			s.Unparsable = "a step must have exactly one key"
			return s
		}
		key, val := n.Content[0].Value, n.Content[1]
		s.Kind = key
		switch {
		case slices.Contains(builtinSteps, key):
			if args := mapGet(val, "extra_args"); args != nil {
				s.ExtraArgs = scalars(args)
			}
		case key == "run" || key == "multienv":
			if val.Kind == yaml.ScalarNode {
				s.Command = val.Value
			} else {
				s.Command = scalarAt(val, "command")
			}
		case key == "env":
			s.EnvName = scalarAt(val, "name")
			if v := mapGet(val, "value"); v != nil {
				s.EnvValue, s.HasEnvVal = v.Value, true
			}
			s.Command = scalarAt(val, "command")
		default:
			s.Unparsable = fmt.Sprintf("unknown step %q", key)
		}
		return s
	}
	s.Unparsable = "unsupported step syntax"
	return s
}

// Atlantis run-step variables that native env values can reference, and
// the native names they map to.
var contextVars = map[string]string{
	"BASE_REPO_OWNER": "BASE_REPO_OWNER",
	"BASE_REPO_NAME":  "BASE_REPO_NAME",
	"REPO_REL_DIR":    "REPO_REL_DIR",
	"WORKSPACE":       "WORKSPACE",
	"PROJECT_NAME":    "PROJECT_NAME",
	"PULL_NUM":        "PULL_NUM",
	"HEAD_COMMIT":     "HEAD_COMMIT",
}

var (
	shellMeta     = regexp.MustCompile("[|;&<>`]|\\$\\(")
	placeholderRe = regexp.MustCompile(`^\s*(echo|printf|true|:)(\s|$)`)
	cleanupRe     = regexp.MustCompile(`^\s*rm\s+-(rf|fr|r)\s+\.terraform/?\s*$`)
	workspaceRe   = regexp.MustCompile(`^\s*(terraform|tofu|terragrunt(\s+run\s+--)?)\s+workspace\s+select\s+(-or-create\s+)?(\$WORKSPACE|\$\{WORKSPACE\}|"\$WORKSPACE")\s*$`)
	builtinCmdRe  = regexp.MustCompile(`^\s*(terraform|tofu|terragrunt)\s+(?:run\s+--\s+)?(init|plan|apply|show)(\s+.*)?$`)
	varRefRe      = regexp.MustCompile(`\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?`)
)

// dropReason returns why a run command can be dropped without losing
// behaviour, or "".
func dropReason(cmd string) string {
	switch {
	case cleanupRe.MatchString(cmd):
		return "removing .terraform is replaced by init -reconfigure when backend_config is set"
	case workspaceRe.MatchString(cmd):
		return "built-in steps select the project's workspace"
	case placeholderRe.MatchString(cmd) && !shellMeta.MatchString(cmd):
		return "it only prints text"
	}
	return ""
}

// builtinArgsFromCommand converts `terraform plan ...` style commands into a
// built-in step and its extra args. Arguments the built-in step adds itself
// are removed. tool is "terragrunt" for `terragrunt plan ...` and
// `terragrunt run -- plan ...`, whose own flags Atlantis sets through the
// environment, and "tofu" for `tofu plan ...`. ok is false when the command
// does more than that.
func builtinArgsFromCommand(cmd string) (tool, step string, args []string, ok bool) {
	m := builtinCmdRe.FindStringSubmatch(cmd)
	if m == nil || shellMeta.MatchString(cmd) {
		return "", "", nil, false
	}
	if m[1] == "terragrunt" || m[1] == "tofu" {
		tool = m[1]
	}
	fields := strings.Fields(m[3])
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		switch {
		case f == "-input=false" || f == "-no-color" || f == "$PLANFILE" || f == "${PLANFILE}" ||
			f == "-out=$PLANFILE" || f == "-out=${PLANFILE}" || f == "$SHOWFILE":
			continue
		case f == "-out" && i+1 < len(fields):
			i++
			continue
		case tool == "terragrunt" && strings.HasPrefix(f, "--"):
			// A Terragrunt flag such as --non-interactive; Terraform's
			// flags use a single dash.
			continue
		case strings.Contains(f, "$"):
			// Any other variable is runtime-dependent; leave it to a human.
			return "", "", nil, false
		}
		args = append(args, f)
	}
	return tool, m[2], args, true
}

// templateFromEcho converts `echo "...$VAR..."` env commands into a native
// templated value. ok is false for anything else.
func templateFromEcho(cmd string) (string, bool) {
	c := strings.TrimSpace(cmd)
	if !strings.HasPrefix(c, "echo ") || shellMeta.MatchString(c) {
		return "", false
	}
	arg := strings.TrimSpace(strings.TrimPrefix(c, "echo "))
	if len(arg) >= 2 && (arg[0] == '"' || arg[0] == '\'') && arg[len(arg)-1] == arg[0] {
		if arg[0] == '\'' {
			return arg[1 : len(arg)-1], !strings.Contains(arg, "$")
		}
		arg = arg[1 : len(arg)-1]
	}
	okAll := true
	out := varRefRe.ReplaceAllStringFunc(arg, func(ref string) string {
		name := varRefRe.FindStringSubmatch(ref)[1]
		native, ok := contextVars[name]
		if !ok {
			okAll = false
			return ref
		}
		return "${" + native + "}"
	})
	return out, okAll
}

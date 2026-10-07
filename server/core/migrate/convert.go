package migrate

import (
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Action says what happened to a piece of the old configuration.
type Action string

const (
	// Converted: now expressed as native inputs.
	Converted Action = "converted"
	// Dropped: removed without losing behaviour.
	Dropped Action = "dropped"
	// Removed: removed; the behaviour must be provided another way.
	Removed Action = "removed"
	// Review: converted or removed, but a person should check the result.
	Review Action = "review"
)

// Note records one migration decision.
type Note struct {
	File     string
	Location string
	Action   Action
	Detail   string
	// Command is the removed shell command, if any.
	Command string
	// Label and Suggestion are filled in by a Labeler for removed commands.
	Label      string
	Confidence float64
	Suggestion string
}

// Inputs is the native inputs block produced for one workflow.
type Inputs struct {
	VarFiles      []string
	Vars          map[string]string
	BackendConfig []string
	Env           map[string]string
	ExtraArgs     map[string][]string
	// Tool is "terragrunt" when the workflow ran Terragrunt. It becomes the
	// `tool` key next to inputs, not part of them.
	Tool string
	// Distribution is "opentofu" when the workflow ran OpenTofu. It becomes
	// the `terraform_distribution` key next to inputs.
	Distribution string
}

// IsZero reports whether neither inputs nor a tool were produced.
func (in Inputs) IsZero() bool {
	return !in.hasInputs() && in.Tool == "" && in.Distribution == ""
}

func (in Inputs) hasInputs() bool {
	return len(in.VarFiles) > 0 || len(in.Vars) > 0 || len(in.BackendConfig) > 0 || len(in.Env) > 0 || len(in.ExtraArgs) > 0
}

// applyTo sets the inputs and tool keys of a repo entry or project.
func (in Inputs) applyTo(m *yaml.Node) {
	if in.hasInputs() {
		mapSet(m, "inputs", in.node())
	}
	if in.Tool != "" {
		mapSet(m, "tool", scalar(in.Tool))
	}
	if in.Distribution != "" {
		mapSet(m, "terraform_distribution", scalar(in.Distribution))
	}
}

func (in *Inputs) addExtra(step string, args ...string) {
	if len(args) == 0 {
		return
	}
	if in.ExtraArgs == nil {
		in.ExtraArgs = map[string][]string{}
	}
	in.ExtraArgs[step] = append(in.ExtraArgs[step], args...)
}

// node renders the inputs block in a fixed key order.
func (in Inputs) node() *yaml.Node {
	m := newMap()
	if len(in.VarFiles) > 0 {
		mapSet(m, "var_files", seqOf(in.VarFiles))
	}
	if len(in.Vars) > 0 {
		v := newMap()
		for _, k := range slices.Sorted(maps.Keys(in.Vars)) {
			mapSet(v, k, scalar(in.Vars[k]))
		}
		mapSet(m, "vars", v)
	}
	if len(in.BackendConfig) > 0 {
		mapSet(m, "backend_config", seqOf(in.BackendConfig))
	}
	if len(in.Env) > 0 {
		e := newMap()
		for _, k := range slices.Sorted(maps.Keys(in.Env)) {
			e.Content = append(e.Content, scalar(k), &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: in.Env[k], Style: yaml.DoubleQuotedStyle})
		}
		mapSet(m, "env", e)
	}
	if len(in.ExtraArgs) > 0 {
		x := newMap()
		for _, k := range slices.Sorted(maps.Keys(in.ExtraArgs)) {
			mapSet(x, k, seqOf(in.ExtraArgs[k]))
		}
		mapSet(m, "extra_args", x)
	}
	return m
}

// workflowConverter converts one workflow's steps.
type workflowConverter struct {
	file, name string
	in         Inputs
	notes      []Note
	// argsBySource remembers which stage set var inputs, to detect stages
	// that disagree (inputs apply to plan and import alike).
	varSource string
	seen      map[string][]string // stage -> built-in steps it runs
	// plainBuiltins counts built-in steps written as steps, not as commands.
	plainBuiltins int
}

func (c *workflowConverter) note(loc string, a Action, detail string, cmd string) {
	c.notes = append(c.notes, Note{File: c.file, Location: fmt.Sprintf("workflows.%s %s", c.name, loc), Action: a, Detail: detail, Command: cmd})
}

// ConvertWorkflow converts a workflow mapping node into native inputs.
func ConvertWorkflow(file, name string, wf *yaml.Node) (Inputs, []Note) {
	c := &workflowConverter{file: file, name: name, seen: map[string][]string{}}
	steps := parseWorkflow(wf)
	for _, s := range steps {
		if _, ok := c.seen[s.Stage]; !ok {
			c.seen[s.Stage] = []string{}
		}
	}
	perStage := map[string]int{}
	for _, s := range steps {
		perStage[s.Stage]++
		loc := fmt.Sprintf("%s step %d (%s)", s.Stage, perStage[s.Stage], s.Kind)
		switch {
		case s.Unparsable != "":
			c.note(loc, Review, "could not read step: "+s.Unparsable, "")
		case slices.Contains(builtinSteps, s.Kind):
			c.plainBuiltins++
			c.builtin(s.Stage, s.Kind, s.ExtraArgs, loc)
		case s.Kind == "run":
			c.run(s, loc)
		case s.Kind == "env":
			c.env(s, loc)
		case s.Kind == "multienv":
			c.note(loc, Removed, "multienv is not supported; set fixed or templated values in inputs.env", s.Command)
		}
	}
	c.checkStages()
	if c.in.Tool != "" && c.plainBuiltins > 0 {
		c.note("", Review, fmt.Sprintf("built-in steps that ran Terraform directly now run through %s too", c.in.Tool), "")
	}
	return c.in, c.notes
}

func (c *workflowConverter) builtin(stage, step string, args []string, loc string) {
	c.seen[stage] = append(c.seen[stage], step)
	switch step {
	case "init":
		var backends, rest []string
		reconfigure := false
		for i := 0; i < len(args); i++ {
			a := args[i]
			switch {
			case strings.HasPrefix(a, "-backend-config="):
				backends = append(backends, strings.TrimPrefix(a, "-backend-config="))
			case a == "-backend-config" && i+1 < len(args):
				i++
				backends = append(backends, args[i])
			case a == "-reconfigure":
				reconfigure = true
			default:
				rest = append(rest, a)
			}
		}
		if len(backends) > 0 {
			if c.in.BackendConfig != nil && !slices.Equal(c.in.BackendConfig, backends) {
				c.note(loc, Review, fmt.Sprintf("stages use different backend config (%v vs %v); kept the first", c.in.BackendConfig, backends), "")
			} else {
				c.in.BackendConfig = backends
			}
		} else if reconfigure {
			rest = append(rest, "-reconfigure")
		}
		if len(rest) > 0 && c.in.ExtraArgs["init"] != nil && !slices.Equal(c.in.ExtraArgs["init"], rest) {
			c.note(loc, Review, fmt.Sprintf("stages pass different init arguments (%v vs %v); kept the first", c.in.ExtraArgs["init"], rest), "")
			rest = nil
		} else if c.in.ExtraArgs["init"] != nil {
			rest = nil
		}
		c.in.addExtra("init", rest...)
	case "plan", "import":
		var files, rest []string
		vars := map[string]string{}
		for i := 0; i < len(args); i++ {
			a := args[i]
			switch {
			case strings.HasPrefix(a, "-var-file="):
				files = append(files, strings.TrimPrefix(a, "-var-file="))
			case a == "-var-file" && i+1 < len(args):
				i++
				files = append(files, args[i])
			case strings.HasPrefix(a, "-var=") && strings.Contains(strings.TrimPrefix(a, "-var="), "="):
				k, v, _ := strings.Cut(strings.TrimPrefix(a, "-var="), "=")
				vars[k] = v
			case a == "-var" && i+1 < len(args) && strings.Contains(args[i+1], "="):
				i++
				k, v, _ := strings.Cut(args[i], "=")
				vars[k] = v
			default:
				rest = append(rest, a)
			}
		}
		if len(files) > 0 || len(vars) > 0 {
			if c.varSource != "" && (!slices.Equal(c.in.VarFiles, files) || !maps.Equal(c.in.Vars, vars)) {
				c.note(loc, Review, fmt.Sprintf("%s uses different variables than %s; native inputs apply the same variables to plan and import, kept %s's", step, c.varSource, c.varSource), "")
			} else {
				c.varSource = step
				c.in.VarFiles, c.in.Vars = files, nilIfEmpty(vars)
			}
		}
		c.in.addExtra(step, rest...)
	default:
		c.in.addExtra(step, args...)
	}
	if len(args) > 0 {
		c.note(loc, Converted, "arguments moved to inputs", "")
	}
}

func nilIfEmpty(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	return m
}

func (c *workflowConverter) run(s Step, loc string) {
	if reason := dropReason(s.Command); reason != "" {
		c.note(loc, Dropped, reason, s.Command)
		return
	}
	if tool, step, args, ok := builtinArgsFromCommand(s.Command); ok {
		c.builtin(s.Stage, step, args, loc)
		switch tool {
		case "terragrunt":
			c.in.Tool = tool
			c.note(loc, Converted, fmt.Sprintf("replaced by the built-in %s step with tool: %s", step, tool), s.Command)
			return
		case "tofu":
			c.in.Distribution = "opentofu"
			c.note(loc, Converted, fmt.Sprintf("replaced by the built-in %s step with terraform_distribution: opentofu", step), s.Command)
			return
		}
		c.note(loc, Converted, fmt.Sprintf("replaced by the built-in %s step", step), s.Command)
		return
	}
	c.note(loc, Removed, "custom run commands are not supported", s.Command)
}

func (c *workflowConverter) env(s Step, loc string) {
	if s.EnvName == "" {
		c.note(loc, Review, "env step without a name", "")
		return
	}
	value, ok := s.EnvValue, s.HasEnvVal
	if !ok {
		value, ok = templateFromEcho(s.Command)
	}
	if !ok {
		c.note(loc, Removed, fmt.Sprintf("env %s is computed by a command; set a fixed or templated value in inputs.env", s.EnvName), s.Command)
		return
	}
	// Terragrunt pointed at OpenTofu is the opentofu distribution: Atlantis
	// sets TG_TF_PATH to the binary it resolved for the project.
	if (s.EnvName == "TG_TF_PATH" || s.EnvName == "TERRAGRUNT_TFPATH") && path.Base(value) == "tofu" {
		c.in.Distribution = "opentofu"
		c.note(loc, Converted, "env "+s.EnvName+" replaced by terraform_distribution: opentofu", s.Command)
		return
	}
	if c.in.Env == nil {
		c.in.Env = map[string]string{}
	}
	if prev, dup := c.in.Env[s.EnvName]; dup && prev != value {
		c.note(loc, Review, fmt.Sprintf("env %s is set to different values in different stages; kept %q", s.EnvName, prev), "")
		return
	}
	c.in.Env[s.EnvName] = value
	c.note(loc, Converted, "env "+s.EnvName+" moved to inputs.env", s.Command)
}

// checkStages flags stages whose built-in steps differ from what a native
// workflow runs, since that changes behaviour.
func (c *workflowConverter) checkStages() {
	for _, stage := range Stages {
		got, ok := c.seen[stage]
		if !ok {
			continue
		}
		want := nativeStageSteps[stage]
		if !slices.Equal(got, want) {
			ran := strings.Join(got, ", ")
			if ran == "" {
				ran = "no built-in steps"
			}
			c.note(stage, Review, fmt.Sprintf("native %s runs %s; this workflow ran %s", stage, strings.Join(want, ", "), ran), "")
		}
	}
}

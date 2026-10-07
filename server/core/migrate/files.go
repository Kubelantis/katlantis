package migrate

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Result is a migrated file and what happened to it.
type Result struct {
	File   string
	Output []byte
	Notes  []Note
	// Workflows maps each converted workflow name to its inputs. Server
	// results are passed to MigrateRepoConfig to resolve server workflows
	// referenced from atlantis.yaml.
	Workflows map[string]Inputs
}

func load(src []byte) (*yaml.Node, *yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return nil, nil, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, nil, fmt.Errorf("expected a YAML mapping at the top level")
	}
	return &doc, doc.Content[0], nil
}

func dump(doc *yaml.Node) ([]byte, error) {
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return b.Bytes(), enc.Close()
}

// lostSteps counts the custom steps removed from each workflow.
func lostSteps(notes []Note) map[string]int {
	out := map[string]int{}
	for _, n := range notes {
		if n.Action == Removed {
			if name, _, ok := strings.Cut(strings.TrimPrefix(n.Location, "workflows."), " "); ok && strings.HasPrefix(n.Location, "workflows.") {
				out[name]++
			}
		}
	}
	return out
}

func convertWorkflows(file string, top *yaml.Node) (map[string]Inputs, []Note) {
	out := map[string]Inputs{}
	var notes []Note
	wfs := mapGet(top, "workflows")
	if wfs == nil || wfs.Kind != yaml.MappingNode {
		return out, nil
	}
	for i := 0; i+1 < len(wfs.Content); i += 2 {
		name := wfs.Content[i].Value
		in, n := ConvertWorkflow(file, name, wfs.Content[i+1])
		out[name] = in
		notes = append(notes, n...)
	}
	return out, notes
}

// describe names what replaced a workflow.
func (in Inputs) describe() string {
	switch {
	case in.hasInputs() && in.Tool != "":
		return "inputs and tool: " + in.Tool
	case in.Tool != "":
		return "tool: " + in.Tool
	}
	return "inputs"
}

// usesTool reports whether any converted workflow sets a tool.
func usesTool(workflows map[string]Inputs) bool {
	for _, in := range workflows {
		if in.Tool != "" {
			return true
		}
	}
	return false
}

// MigrateServerConfig migrates a server-side repos.yaml.
func MigrateServerConfig(file string, src []byte) (*Result, error) {
	doc, top, err := load(src)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	r := &Result{File: file}
	r.Workflows, r.Notes = convertWorkflows(file, top)
	add := func(loc string, a Action, detail, cmd string) {
		r.Notes = append(r.Notes, Note{File: file, Location: loc, Action: a, Detail: detail, Command: cmd})
	}

	repos := mapGet(top, "repos")
	if repos != nil && repos.Kind == yaml.SequenceNode {
		for i, entry := range repos.Content {
			loc := fmt.Sprintf("repos[%d] %s", i, scalarAt(entry, "id"))
			if name := scalarAt(entry, "workflow"); name != "" {
				mapDelete(entry, "workflow")
				in, ok := r.Workflows[name]
				if lost := lostSteps(r.Notes)[name]; lost > 0 {
					add(loc, Review, fmt.Sprintf("workflow %q lost %d custom step(s); see the removed items", name, lost), "")
				}
				switch {
				case !ok:
					add(loc, Review, fmt.Sprintf("workflow %q is not defined in this file; set inputs by hand", name), "")
				case in.hasInputs() && mapGet(entry, "inputs") != nil:
					add(loc, Review, fmt.Sprintf("already has inputs; workflow %q was not merged into them", name), "")
				case !in.IsZero():
					in.applyTo(entry)
					add(loc, Converted, fmt.Sprintf("workflow %q replaced by %s", name, in.describe()), "")
				default:
					add(loc, Dropped, fmt.Sprintf("workflow %q needs no inputs", name), "")
				}
			}
			if ov := mapGet(entry, "allowed_overrides"); ov != nil {
				vals := scalars(ov)
				if j := slices.Index(vals, "workflow"); j >= 0 {
					vals = slices.Delete(vals, j, j+1)
					if !slices.Contains(vals, "inputs") {
						vals = append(vals, "inputs")
					}
					if usesTool(r.Workflows) && !slices.Contains(vals, "tool") {
						vals = append(vals, "tool")
					}
					mapSet(entry, "allowed_overrides", seqOf(vals))
					add(loc, Converted, "allowed_overrides: workflow replaced by inputs", "")
				}
				if slices.Contains(vals, "custom_policy_check") {
					vals = slices.DeleteFunc(vals, func(v string) bool { return v == "custom_policy_check" })
					mapSet(entry, "allowed_overrides", seqOf(vals))
				}
			}
			for _, key := range []string{"allowed_workflows", "allow_custom_workflows"} {
				if mapDelete(entry, key) {
					add(loc, Dropped, key+" no longer exists; projects set inputs directly", "")
				}
			}
			if mapDelete(entry, "custom_policy_check") {
				add(loc, Removed, "custom_policy_check is not supported; policies run with the built-in conftest policy_check", "")
			}
			// Workflow hooks are server-side and stay: they are where commands
			// that must run in Atlantis's clone belong now.
		}
	}

	// A server-side workflow named "default" applied to every repo without
	// its own workflow. A first catch-all entry reproduces that: inputs come
	// from the last matching entry that sets them.
	if in, ok := r.Workflows["default"]; ok && !in.IsZero() {
		if repos == nil {
			repos = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			mapSet(top, "repos", repos)
		}
		entry := newMap()
		mapSet(entry, "id", scalar("/.*/"))
		in.applyTo(entry)
		repos.Content = append([]*yaml.Node{entry}, repos.Content...)
		add("repos", Converted, `workflow "default" replaced by a first catch-all entry with inputs`, "")
	}
	if mapDelete(top, "workflows") {
		add("workflows", Dropped, "custom workflows are replaced by inputs", "")
	}
	if r.Output, err = dump(doc); err != nil {
		return nil, err
	}
	return r, nil
}

// MigrateRepoConfig migrates a repo-level atlantis.yaml. server holds the
// converted server-side workflows (may be nil).
func MigrateRepoConfig(file string, src []byte, server map[string]Inputs) (*Result, error) {
	doc, top, err := load(src)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	r := &Result{File: file}
	r.Workflows, r.Notes = convertWorkflows(file, top)
	add := func(loc string, a Action, detail string) {
		r.Notes = append(r.Notes, Note{File: file, Location: loc, Action: a, Detail: detail})
	}
	projects := mapGet(top, "projects")
	if projects != nil && projects.Kind == yaml.SequenceNode {
		for i, p := range projects.Content {
			loc := fmt.Sprintf("projects[%d] %s", i, scalarAt(p, "dir"))
			if name := scalarAt(p, "workflow"); name != "" {
				mapDelete(p, "workflow")
				in, local := r.Workflows[name]
				if !local {
					in, local = server[name]
					if local {
						add(loc, Review, fmt.Sprintf("workflow %q is server-side; its settings were copied here, which needs them in allowed_overrides (inputs, tool)", name))
					}
				}
				if lost := lostSteps(r.Notes)[name]; lost > 0 && local {
					add(loc, Review, fmt.Sprintf("workflow %q lost %d custom step(s); see the removed items", name, lost))
				}
				switch {
				case !local:
					add(loc, Review, fmt.Sprintf("workflow %q is defined elsewhere; migrate the server config with this file, or set inputs by hand", name))
				case in.hasInputs() && mapGet(p, "inputs") != nil:
					add(loc, Review, fmt.Sprintf("already has inputs; workflow %q was not merged into them", name))
				case !in.IsZero():
					in.applyTo(p)
					add(loc, Converted, fmt.Sprintf("workflow %q replaced by %s", name, in.describe()))
				default:
					add(loc, Dropped, fmt.Sprintf("workflow %q needs no inputs", name))
				}
			}
			if mapDelete(p, "custom_policy_check") {
				add(loc, Removed, "custom_policy_check is not supported; policies run with the built-in conftest policy_check")
			}
		}
	}
	if mapDelete(top, "workflows") {
		add("workflows", Dropped, "custom workflows are replaced by inputs")
	}
	if r.Output, err = dump(doc); err != nil {
		return nil, err
	}
	return r, nil
}

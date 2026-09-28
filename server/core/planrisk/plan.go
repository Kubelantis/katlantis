package planrisk

import (
	"encoding/json"
	"fmt"
	"slices"
)

// Action is the effect of a plan on one resource.
type Action string

const (
	ActionCreate  Action = "create"
	ActionUpdate  Action = "update"
	ActionDelete  Action = "delete"
	ActionReplace Action = "replace"
)

// destructive actions can lose data or cause downtime.
func (a Action) destructive() bool { return a == ActionDelete || a == ActionReplace }

// severity orders actions so the most dangerous are assessed first.
func (a Action) severity() int {
	switch a {
	case ActionReplace:
		return 3
	case ActionDelete:
		return 2
	case ActionUpdate:
		return 1
	}
	return 0
}

// Change is one resource change, reduced to what the model needs. Attribute
// values are deliberately excluded: they can contain secrets.
type Change struct {
	Address string `json:"address"`
	Type    string `json:"type"`
	Action  Action `json:"action"`
}

// showJSON is the subset of `terraform show -json <planfile>` we read.
type showJSON struct {
	ResourceChanges []struct {
		Address string `json:"address"`
		Mode    string `json:"mode"`
		Type    string `json:"type"`
		Change  struct {
			Actions []string `json:"actions"`
		} `json:"change"`
	} `json:"resource_changes"`
}

// ParseChanges extracts the managed-resource changes from terraform show
// JSON, most destructive first. No-op and read actions are dropped.
func ParseChanges(showOutput []byte) ([]Change, error) {
	var plan showJSON
	if err := json.Unmarshal(showOutput, &plan); err != nil {
		return nil, fmt.Errorf("parsing terraform show output: %w", err)
	}
	var changes []Change
	for _, rc := range plan.ResourceChanges {
		if rc.Mode != "" && rc.Mode != "managed" {
			continue
		}
		action, ok := classify(rc.Change.Actions)
		if !ok {
			continue
		}
		changes = append(changes, Change{Address: rc.Address, Type: rc.Type, Action: action})
	}
	slices.SortStableFunc(changes, func(a, b Change) int { return b.Action.severity() - a.Action.severity() })
	return changes, nil
}

func classify(actions []string) (Action, bool) {
	switch {
	case slices.Equal(actions, []string{"create"}):
		return ActionCreate, true
	case slices.Equal(actions, []string{"update"}):
		return ActionUpdate, true
	case slices.Equal(actions, []string{"delete"}):
		return ActionDelete, true
	case slices.Equal(actions, []string{"delete", "create"}), slices.Equal(actions, []string{"create", "delete"}):
		return ActionReplace, true
	}
	return "", false
}

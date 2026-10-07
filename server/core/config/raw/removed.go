package raw

import (
	"fmt"
	"maps"
	"slices"

	"go.yaml.in/yaml/v4"
)

// MigrateHint is appended to errors about configuration that native
// workflows replaced.
const MigrateHint = "run `atlantis migrate-workflows` to convert it; see https://www.runatlantis.io/docs/server-side-repo-config.html#migrating-custom-workflows-to-native-inputs"

// Removed is a key that no longer exists. Decoding accepts any value so
// validation can say what replaced it instead of reporting an unknown field.
type Removed struct {
	set bool
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (r *Removed) UnmarshalYAML(*yaml.Node) error {
	r.set = true
	return nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *Removed) UnmarshalJSON([]byte) error {
	r.set = true
	return nil
}

// IsSet reports whether the key was present.
func (r Removed) IsSet() bool {
	return r.set
}

// removedKeys returns an error naming the first key that is set.
func removedKeys(keys map[string]Removed) error {
	for _, k := range slices.Sorted(maps.Keys(keys)) {
		if keys[k].IsSet() {
			return fmt.Errorf("%s was removed: %s; %s", k, removedReplacement[k], MigrateHint)
		}
	}
	return nil
}

// What replaced each removed key.
var removedReplacement = map[string]string{
	"workflows":              "use native inputs (`inputs`) and `tool` instead of custom workflows; commands that must run in Atlantis's clone go in server-side pre_workflow_hooks or post_workflow_hooks",
	"workflow":               "use native inputs (`inputs`) and `tool` instead of selecting a workflow",
	"allowed_workflows":      "custom workflows no longer exist; use `allowed_overrides: [inputs]`",
	"allow_custom_workflows": "custom workflows no longer exist; use `allowed_overrides: [inputs]`",
	"custom_policy_check":    "policies run with the built-in Conftest policy_check",
}

package tfclient

import (
	"errors"
	"fmt"
	"os/exec"
)

// errTerragruntMissing is returned when a project uses Terragrunt but the
// binary is not installed.
var errTerragruntMissing = errors.New("project uses tool \"terragrunt\" but no terragrunt binary was found on PATH; install it in the Atlantis image")

// terragruntPath locates the terragrunt binary.
func (c *DefaultClient) terragruntPath() (string, error) {
	if c.overrideTerragrunt != "" {
		return c.overrideTerragrunt, nil
	}
	p, err := exec.LookPath("terragrunt")
	if err != nil {
		return "", errTerragruntMissing
	}
	return p, nil
}

// terragruntCommand is the command prefix for a Terragrunt project. `run --`
// passes the Terraform command through unchanged: Terragrunt 1.x rejects
// commands it has no shortcut for, such as `workspace`.
func terragruntCommand(tg string) []string {
	return []string{tg, "run", "--"}
}

// terragruntEnv points Terragrunt at tfBinary and makes it behave like the
// binary it wraps: no prompts, Terraform's output unprefixed (so plan
// parsing and comments work), and only errors logged (so `show -json`
// output stays valid JSON).
func terragruntEnv(tfBinary string) []string {
	return []string{
		fmt.Sprintf("TG_TF_PATH=%s", tfBinary),
		"TG_NON_INTERACTIVE=true",
		"TG_TF_FORWARD_STDOUT=true",
		"TG_LOG_LEVEL=error",
		"TG_NO_COLOR=true",
	}
}

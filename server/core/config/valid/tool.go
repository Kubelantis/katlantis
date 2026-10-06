package valid

// ToolKey is the allowed_overrides key that lets repo config choose the tool.
const ToolKey = "tool"

// IaC tools that run a project's built-in steps.
const (
	// ToolTerraform runs the project's Terraform or OpenTofu binary directly.
	ToolTerraform = "terraform"
	// ToolTerragrunt runs Terragrunt, pointed at the project's Terraform or
	// OpenTofu binary.
	ToolTerragrunt = "terragrunt"
	// ToolCdktn synthesizes a CDK Terrain app and runs the project's
	// Terraform or OpenTofu binary in the synthesized stack.
	ToolCdktn = "cdktn"
)

// Tools lists the supported tools.
var Tools = []string{ToolTerraform, ToolTerragrunt, ToolCdktn}

include "root" {
  path = find_in_parent_folders("root.hcl")
}

terraform {
  source = "../../modules/net"
}

dependency "vpc" {
  config_path = "../vpc"

  mock_outputs = {
    id = "pending-vpc"
  }
  mock_outputs_allowed_terraform_commands = ["init", "plan", "show", "workspace"]
}

inputs = {
  name   = "app"
  vpc_id = dependency.vpc.outputs.id
}

variable "env_name" {}
variable "team" {}

resource "null_resource" "terragrunt" {}

output "env_name" {
  value = var.env_name
}

output "team" {
  value = var.team
}

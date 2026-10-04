terraform {
  backend "local" {}
}

variable "env_name" {}
variable "team" {}
variable "repo_tag" {}

resource "null_resource" "native" {}

output "env_name" {
  value = var.env_name
}

output "team" {
  value = var.team
}

output "repo_tag" {
  value = var.repo_tag
}

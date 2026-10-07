# Only OpenTofu reads .tofu files: a plan that creates this resource ran OpenTofu.
resource "terraform_data" "engine" {
  input = "opentofu"
}

output "engine" {
  value = terraform_data.engine.output
}

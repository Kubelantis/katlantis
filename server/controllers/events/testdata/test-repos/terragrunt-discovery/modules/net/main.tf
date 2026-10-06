variable "env" {}
variable "name" {}
variable "vpc_id" {
  default = ""
}

resource "terraform_data" "net" {
  input = "${var.env}-${var.name}${var.vpc_id == "" ? "" : "-in-${var.vpc_id}"}"
}

output "id" {
  value = terraform_data.net.output
}

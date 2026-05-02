---
tags:
  - modules
  - consuming
  - guides
---

# Consuming Modules

Once modules are synced, reference them in your OpenTofu or Terraform configuration:

```hcl
module "eks" {
  source  = "opendepot.defdev.io/opendepot-system/terraform-aws-eks/aws"
  version = "~> 21.0"
}

module "aks" {
  source  = "opendepot.defdev.io/opendepot-system/terraform-azurerm-aks/azurerm"
  version = ">= 10.0.0"
}
```

The source format is `<registry-host>/<namespace>/<name>/<provider>`, where `<namespace>` is the Kubernetes namespace where the `Module` resource lives.

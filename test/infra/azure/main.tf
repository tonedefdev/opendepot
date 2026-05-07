terraform {
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "~> 4.0"
    }
    time = {
      source  = "hashicorp/time"
      version = "~> 0.12"
    }
  }
}

# Locally: authenticates via Azure CLI (`az login`).
# In CI: authenticates via OIDC when ARM_USE_OIDC=true and ARM_CLIENT_ID /
# ARM_TENANT_ID / ARM_SUBSCRIPTION_ID are set in the environment.
provider "azurerm" {
  subscription_id = var.subscription_id
  features {}
}

resource "azurerm_resource_group" "integration" {
  name     = "opendepot-integration-${var.suffix}"
  location = var.location

  tags = {
    Purpose   = "opendepot-storage-integration-tests"
    ManagedBy = "opentofu"
  }
}

resource "azurerm_storage_account" "integration" {
  # Storage account names: 3–24 chars, lowercase letters and numbers only.
  # Truncate to 24 chars — the nanosecond suffix provides enough uniqueness.
  account_replication_type = "LRS"
  account_tier             = "Standard"
  location                 = azurerm_resource_group.integration.location
  name                     = lower(substr("opendepotintg${var.suffix}", 0, 24))
  resource_group_name      = azurerm_resource_group.integration.name

  tags = {
    Purpose   = "opendepot-storage-integration-tests"
    ManagedBy = "opentofu"
  }
}

# Resolve the identity (user or service principal) that is running OpenTofu.
# Both roles are scoped to the storage account so the test principal has
# data-plane access for blob upload/download/delete and for generating
# User Delegation SAS URLs (PresignObject).
data "azurerm_client_config" "current" {}

resource "azurerm_role_assignment" "blob_data_contributor" {
  principal_id         = data.azurerm_client_config.current.object_id
  role_definition_name = "Storage Blob Data Contributor"
  scope                = azurerm_storage_account.integration.id
}

resource "azurerm_role_assignment" "blob_delegator" {
  principal_id         = data.azurerm_client_config.current.object_id
  role_definition_name = "Storage Blob Delegator"
  scope                = azurerm_storage_account.integration.id
}

# Azure RBAC can take up to a few minutes to propagate after assignment.
# Block InitAndApply for 90 s so the test principal has effective data-plane
# access before Terratest starts executing sub-tests.
resource "time_sleep" "rbac_propagation" {
  create_duration = "90s"

  depends_on = [
    azurerm_role_assignment.blob_data_contributor,
    azurerm_role_assignment.blob_delegator,
  ]
}

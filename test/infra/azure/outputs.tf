output "account_name" {
  value = azurerm_storage_account.integration.name
}

output "account_url" {
  value = azurerm_storage_account.integration.primary_blob_endpoint
}

output "resource_group_name" {
  value = azurerm_resource_group.integration.name
}

variable "location" {
  description = "Azure region for the resource group and storage account."
  type        = string
  default     = "West US 2"
}

variable "suffix" {
  description = "Unique suffix appended to resource names to prevent collisions across parallel runs."
  type        = string
}

variable "subscription_id" {
  description = "Azure subscription ID that owns the resources."
  type        = string
}

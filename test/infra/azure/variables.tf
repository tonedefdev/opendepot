variable "suffix" {
  type        = string
  description = "Unique suffix appended to resource names to prevent collisions across parallel runs."
}

variable "location" {
  type        = string
  description = "Azure region for the resource group and storage account."
  default     = "West US 2"
}

variable "subscription_id" {
  type        = string
  description = "Azure subscription ID that owns the resources."
}

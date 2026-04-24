variable "neon_api_key" {
  type        = string
  sensitive   = true
  description = "Neon API key from https://console.neon.tech/app/settings/api-keys"
}

variable "ghcr_username" {
  type        = string
  description = "GitHub username for ghcr.io registry"
}

variable "ghcr_token" {
  type        = string
  sensitive   = true
  description = "GitHub PAT with read:packages scope"
}

variable "app_image" {
  type        = string
  description = "Full container image reference, e.g. ghcr.io/youruser/ape-trader-api:latest"
}

variable "location" {
  type        = string
  default     = "westeurope"
  description = "Azure region"
}

variable "project_name" {
  type        = string
  default     = "ape-trader"
  description = "Base name used for all Azure resources"
}

variable "neon_region" {
  type        = string
  default     = "aws-eu-central-1"
  description = "Neon region — pick closest to Azure region. See: https://neon.tech/docs/introduction/regions"
}

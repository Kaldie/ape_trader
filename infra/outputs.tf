output "app_url" {
  value       = "https://${azurerm_container_app.api.ingress[0].fqdn}"
  description = "Public URL of the deployed API"
}

output "neon_connection_uri" {
  value       = neon_project.main.connection_uri
  sensitive   = true
  description = "Full Neon PostgreSQL connection URI (use: terraform output -raw neon_connection_uri)"
}

output "neon_host" {
  value       = neon_project.main.database_host
  description = "Neon database hostname"
}

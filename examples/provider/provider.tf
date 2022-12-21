# Initialization of kubernetes clients
provider "liqo" {
  kubernetes = {
    config_path = "path/to/kubeconfig"
  }
}
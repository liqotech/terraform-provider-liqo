# Create a peering relationship
resource "liqo_peer" "basic_peer" {
  remote_kubeconfig = "/path/to/remote/kubeconfig"
  remote_context    = "my-remote-context"
}

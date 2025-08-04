# Liqo Terraform Provider

> Official Terraform provider for Liqo operations, enabling seamless resource management across Kubernetes clusters.

## Overview

The Liqo Terraform Provider allows you to manage Liqo resources declaratively using Terraform. It supports:

- **Cluster Peering**: Establish secure peering relationships between Kubernetes clusters
- **Namespace Offloading**: Extend namespaces across cluster boundaries

## Features

- ✅ Peer resource with automatic status monitoring
- ✅ Offload resource for namespace extension
- ✅ Built-in timeout and error handling

## Prerequisites

- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 0.14
- [Go](https://go.dev/doc/install) >= 1.19 (for building from source)
- [Liqo](https://liqo.io) installed on your Kubernetes clusters
- Valid kubeconfig access to your clusters

## Installation

### Method 1: Using Terraform Registry (Recommended)

```terraform
terraform {
  required_providers {
    liqo = {
      source  = "liqotech/liqo"
      version = "~> 0.1.0"
    }
  }
}
```

### Method 2: Local Development Build

1. Create the local plugin directory:

   ```bash
   mkdir -p ~/.terraform.d/plugins/liqo-provider/liqo/liqo/0.0.1/<architecture>/
   ```

   Replace `<architecture>` with your system architecture (e.g., `linux_amd64`, `linux_arm64`, `darwin_amd64`)

2. Build and install the provider:

   ```bash
   go build -o ~/.terraform.d/plugins/liqo-provider/liqo/liqo/0.0.1/<architecture>/terraform-provider-liqo
   ```

3. Configure Terraform to use the local provider:

   ```terraform
   terraform {
     required_providers {
       liqo = {
         source = "liqo-provider/liqo/liqo"
       }
     }
   }
   ```

## Quick Start

```terraform
# Configure the Liqo Provider
provider "liqo" {
  kubernetes = {
    config_path = "~/.kube/config"
  }
}

# Establish a peer relationship
resource "liqo_peer" "example" {
  remote_kubeconfig = "/path/to/remote/kubeconfig"
  timeout           = "10m"  # Optional: wait timeout for peering completion
}

# Offload a namespace
resource "liqo_offload" "example" {
  namespace                = "my-namespace"
  namespace_mapping_strategy = "EnforceSameName"
  pod_offloading_strategy   = "Remote"
  
  depends_on = [liqo_peer.example]
}
```

## Documentation

Detailed documentation is available in the [`docs/`](./docs/) directory:

- [Provider Configuration](./docs/index.md)
- [Peer Resource](./docs/resources/peer.md)
- [Offload Resource](./docs/resources/offload.md)

## Examples

Complete examples are available in the [`examples/`](./examples/) directory:

- [Basic Peering](./examples/resources/peering_resource/)
- [Namespace Offloading](./examples/resources/offload_resource/)

## Project Structure

```text
├── docs/                    # Documentation
│   ├── index.md            # Provider documentation
│   └── resources/          # Resource-specific docs
├── examples/               # Usage examples
│   ├── provider/          # Provider configuration examples
│   └── resources/         # Resource usage examples
├── liqo/                  # Provider source code
│   ├── provider.go        # Provider implementation
│   ├── peer_resource.go   # Peer resource
│   ├── peer_resource_status.go  # Peer status monitoring
│   ├── offload_resource.go      # Offload resource
│   └── attribute_plan_modifier/  # Custom plan modifiers
└── main.go               # Provider entry point
```

## Development

### Building the Provider

1. **Generate version constants** (required before building):

   ```bash
   go generate ./liqo
   ```

   This automatically extracts the liqo version from `go.mod` and generates compile-time constants.

2. **Build the provider**:

   ```bash
   go build -o terraform-provider-liqo
   ```

### Version Management

The provider automatically uses the same liqo version as specified in `go.mod` for downloading `liqoctl` binaries. This is achieved through:

- **Compile-time extraction**: The `go generate` command extracts the liqo version from `go.mod`
- **Generated constants**: Version information is embedded as Go constants during build
- **Zero runtime overhead**: No file parsing or version detection at runtime

When you update the liqo dependency version:

```bash
go mod edit -require=github.com/liqotech/liqo@v1.0.2
go mod tidy
go generate ./liqo  # Updates version constants
go build           # Build with new version
```

### Running Tests

```bash
go test ./...
```

### Contributing

Contributions are welcome! Please see our contributing guidelines and submit pull requests to the main repository.

## Support

For issues and questions:

- [GitHub Issues](https://github.com/liqotech/terraform-provider-liqo/issues)
- [Liqo Documentation](https://docs.liqo.io)

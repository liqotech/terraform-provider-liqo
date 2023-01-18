// Package main creates the provider instances in Terraform.
package main

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/liqotech/terraform-provider-liqo/liqo"
)

// Provider documentation generation.
//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-name liqo

func main() {
	//nolint:errcheck,gosec // Terraform Framework template code
	providerserver.Serve(context.Background(), liqo.New, providerserver.ServeOpts{
		Address: "liqo-provider/liqo/test",
	})
}

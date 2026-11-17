// Command terraform-provider-onplo is Onplo's OpenTofu provider: a customer writes their Onplo
// setup in a file, and OpenTofu makes Onplo match it. It runs in Terraform too; both speak the
// same plugin protocol.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/onplo-official/terraform-provider-onplo/internal/provider"
	"onplo.com/go-client/generated/operations"
)

// version is written into release builds by the linker (-X main.version=...).
var version = "development build"

func main() {
	debug := flag.Bool("debug", false, "run with support for debuggers such as delve")
	flag.Parse()
	err := providerserver.Serve(context.Background(), provider.New(version, operations.All), providerserver.ServeOpts{
		// The address both registries list it under: the GitHub organisation, then its name.
		Address: "registry.opentofu.org/onplo-official/onplo",
		Debug:   *debug,
	})
	if err != nil {
		log.Fatal(err)
	}
}

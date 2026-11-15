package main

import (
	"context"
	"flag"
	"log"

	"github.com/byteshuttle/terraform-provider-allot/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

var version = "dev"

func main() {
	debug := flag.Bool("debug", false, "Enable debugger reattachment")
	flag.Parse()
	if err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{Address: "registry.terraform.io/byteshuttle/allot", Debug: *debug}); err != nil {
		log.Fatal(err)
	}
}

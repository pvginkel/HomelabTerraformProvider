package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/pvginkel/HomelabTerraformProvider/internal/provider"
)

var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "Run with debugger support (delve).")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/pvginkel/homelab",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}

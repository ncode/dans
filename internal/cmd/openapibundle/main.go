package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/oapi-codegen/oapi-codegen/v2/pkg/util"
)

func main() {
	source := flag.String("source", "", "PowerDNS OpenAPI source")
	overlay := flag.String("overlay", "", "DANS OpenAPI overlay")
	output := flag.String("output", "", "combined OpenAPI output")
	flag.Parse()

	if *source == "" || *overlay == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "source, overlay, and output are required")
		os.Exit(2)
	}
	document, err := bundle(*source, *overlay)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bundle OpenAPI: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*output, document, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write OpenAPI bundle: %v\n", err)
		os.Exit(1)
	}
}

func bundle(source, overlay string) ([]byte, error) {
	document, err := util.LoadSwaggerWithOverlay(source, util.LoadSwaggerWithOverlayOpts{
		Path:   overlay,
		Strict: true,
	})
	if err != nil {
		return nil, err
	}
	if err := document.Validate(context.Background()); err != nil {
		return nil, fmt.Errorf("validate combined document: %w", err)
	}

	result, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal combined document: %w", err)
	}
	return append(result, '\n'), nil
}

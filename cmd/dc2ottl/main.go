// Command dc2ottl fetches the derived columns defined on a Honeycomb dataset or
// environment and translates them into an OTTL transform-processor config block,
// plus a report of anything that could not be translated.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/honeycombio/derived-column-translator-processor/pkg/emit"
	"github.com/honeycombio/derived-column-translator-processor/pkg/honeycomb"
	"github.com/honeycombio/derived-column-translator-processor/pkg/translate"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		apiKey     = flag.String("api-key", os.Getenv("HONEYCOMB_API_KEY"), "Honeycomb API key (or set HONEYCOMB_API_KEY)")
		apiURL     = flag.String("api-url", honeycomb.DefaultAPIURL, "Honeycomb API base URL")
		dataset    = flag.String("dataset", honeycomb.AllDatasets, "dataset slug, or __all__ for environment-wide derived columns")
		name       = flag.String("name", "derived_columns", "transform processor name suffix")
		errorMode  = flag.String("error-mode", "ignore", "transform processor error_mode (ignore|silent|propagate)")
		reportPath = flag.String("report", "", "write the translation report to this file (default: stderr)")
	)
	flag.Parse()

	if *apiKey == "" {
		return fmt.Errorf("an API key is required: pass -api-key or set HONEYCOMB_API_KEY")
	}

	client := honeycomb.NewClient(*apiKey, honeycomb.WithAPIURL(*apiURL))
	cols, err := client.ListDerivedColumns(context.Background(), *dataset)
	if err != nil {
		return err
	}

	inputs := make([]emit.Input, len(cols))
	for i, c := range cols {
		inputs[i] = emit.Input{Alias: c.Alias, Expression: c.Expression}
	}

	out := emit.Generate(inputs, translate.DefaultResolver)

	// Config goes to stdout so it can be piped/redirected into a config file.
	fmt.Print(out.TransformConfig(*name, *errorMode))

	report := out.Report()
	if *reportPath != "" {
		if err := os.WriteFile(*reportPath, []byte(report), 0o644); err != nil {
			return fmt.Errorf("writing report: %w", err)
		}
	} else {
		fmt.Fprintln(os.Stderr, "\n"+report)
	}
	return nil
}

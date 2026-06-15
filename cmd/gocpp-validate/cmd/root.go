// Package cmd implements the gocpp-validate CLI.
package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/v16"
	"github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v21"
	"github.com/spf13/cobra"
)

func registry() (*schema.Registry, error) {
	r := schema.NewRegistry()
	for _, reg := range []func(*schema.Registry) error{v16.RegisterSchemas, v201.RegisterSchemas, v21.RegisterSchemas} {
		if err := reg(r); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// RunValidate validates a JSON file against the schema for version/action/kind.
func RunValidate(out io.Writer, version, action, kind, file string) error {
	r, err := registry()
	if err != nil {
		return err
	}
	v, ok := r.Lookup(version, action, kind)
	if !ok {
		return fmt.Errorf("no schema for %s/%s/%s", version, action, kind)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	if err := v.Validate(data); err != nil {
		return fmt.Errorf("invalid: %w", err)
	}
	_, _ = fmt.Fprintf(out, "%s: valid against %s %s (%s)\n", file, version, action, kind)
	return nil
}

// Root builds the cobra command tree.
func Root() *cobra.Command {
	var version, action, kind string
	root := &cobra.Command{
		Use:   "gocpp-validate [file]",
		Short: "Validate an OCPP JSON message against its schema",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return RunValidate(c.OutOrStdout(), version, action, kind, args[0])
		},
	}
	root.Flags().StringVar(&version, "version", "2.0.1", "OCPP version (1.6, 2.0.1, 2.1)")
	root.Flags().StringVar(&action, "action", "", "OCPP action (e.g. BootNotification)")
	root.Flags().StringVar(&kind, "kind", "request", "payload kind (request|response)")
	_ = root.MarkFlagRequired("action")
	return root
}

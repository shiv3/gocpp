// Package cmd implements the gocpp-sim CLI.
package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/shiv3/gocpp/cmd/gocpp-sim/sim"
	"github.com/spf13/cobra"
)

// Root builds the cobra command tree.
func Root() *cobra.Command {
	root := &cobra.Command{Use: "gocpp-sim", Short: "OCPP charge point simulator"}
	root.AddCommand(runCmd())
	return root
}

func runCmd() *cobra.Command {
	var scenarioFile string
	c := &cobra.Command{
		Use:   "run",
		Short: "Run a scenario file",
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := os.ReadFile(scenarioFile)
			if err != nil {
				return err
			}
			sc, err := sim.ParseScenario(b)
			if err != nil {
				return err
			}
			results, err := sim.Run(context.Background(), sc)
			if err != nil {
				return err
			}
			for _, r := range results {
				if r.Err != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "%s: ERROR %v\n", r.Action, r.Err)
					continue
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", r.Action, string(r.Response))
			}
			return nil
		},
	}
	c.Flags().StringVarP(&scenarioFile, "scenario", "s", "", "scenario YAML file")
	_ = c.MarkFlagRequired("scenario")
	return c
}

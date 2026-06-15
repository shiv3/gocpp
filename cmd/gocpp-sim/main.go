// Command gocpp-sim simulates an OCPP charge point.
package main

import (
	"os"

	"github.com/shiv3/gocpp/cmd/gocpp-sim/cmd"
)

func main() {
	if err := cmd.Root().Execute(); err != nil {
		os.Exit(1)
	}
}

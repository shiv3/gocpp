// Command gocpp-validate validates OCPP JSON messages against official schemas.
package main

import (
	"os"

	"github.com/shiv3/gocpp/cmd/gocpp-validate/cmd"
)

func main() {
	if err := cmd.Root().Execute(); err != nil {
		os.Exit(1)
	}
}

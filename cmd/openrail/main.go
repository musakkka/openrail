// SPDX-License-Identifier: MIT

// Command openrail is the openrail command line client. Installed as `railway`
// it also accepts Railway's verbs and flags.
package main

import (
	"os"

	"github.com/musakkka/openrail/internal/cli"
)

func main() { os.Exit(cli.Main()) }

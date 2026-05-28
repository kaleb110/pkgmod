// dep-syncer scans a JavaScript/TypeScript source tree, compares the imports
// it finds against the project's package.json, and installs anything missing.
//
// Usage:
//
//	dep-syncer --src=./src --manager=bun
//
// The tool reads package.json from the current working directory (the project
// root), while --src controls which subdirectory is scanned for source files.
// This matches the standard layout where package.json sits at the root and
// source code lives under src/.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/kaleb110/cmd"
	"github.com/kaleb110/installer"
)

func main() {
	// Parse the --manager flag here, in main, so that unsupported values are
	// rejected immediately — before any scanning or file I/O takes place.
	// All other flags are owned by cmd.Run.
	managerFlag := flag.String("manager", "pnpm", "Package manager to use (pnpm, bun, npm, yarn)")
	flag.Parse()

	m, err := installer.ParseManager(*managerFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	inst, err := installer.New(m)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Pass the remaining (non-manager) args and the concrete installer to the
	// CLI runner.  cmd.Run knows nothing about which manager was chosen.
	if err := cmd.Run(flag.Args(), inst); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
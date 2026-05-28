// dep-syncer scans a JavaScript/TypeScript source tree, compares the imports
// it finds against the project's package.json, and installs anything missing.
//
// Usage:
//
//	dep-syncer --src=./src
//
// The tool reads package.json from the current working directory (the project
// root), while --src controls which subdirectory is scanned for source files.
// This matches the standard layout where package.json sits at the root and
// source code lives under src/.
package main

import (
	"fmt"
	"os"

	"github.com/kaleb110/cmd"
	"github.com/kaleb110/installer"
)

func main() {
	// Wire the real pnpm installer and hand off to the CLI runner.
	// Keeping main() minimal means the entire program logic is testable
	// through cmd.Run without spawning a subprocess.
	if err := cmd.Run(os.Args[1:], &installer.Pnpm{}); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
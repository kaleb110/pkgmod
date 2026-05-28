// Package scanner walks a JavaScript/TypeScript source tree and extracts the
// set of third-party package names referenced by import and require statements.
//
// It is intentionally decoupled from the rest of the tool: it knows nothing
// about package.json, installers, or the CLI.  This makes it independently
// testable and reusable (e.g. if a future version adds a --dry-run flag that
// only prints what would be installed, scanner is unchanged).
package scanner

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// nodeBuiltins is the complete set of Node.js core module names.  Imports of
// these should never be forwarded to a package manager.
var nodeBuiltins = map[string]bool{
	"assert": true, "buffer": true, "child_process": true, "cluster": true,
	"console": true, "crypto": true, "dgram": true, "dns": true, "domain": true,
	"events": true, "fs": true, "http": true, "http2": true, "https": true,
	"module": true, "net": true, "os": true, "path": true, "perf_hooks": true,
	"process": true, "punycode": true, "querystring": true, "readline": true,
	"repl": true, "stream": true, "string_decoder": true, "timers": true,
	"tls": true, "tty": true, "url": true, "util": true, "v8": true,
	"vm": true, "worker_threads": true, "zlib": true,
}

// importRe captures the module specifier string in every common JS/TS import
// and require form:
//
//	import "pkg"                    — side-effect import
//	import X from "pkg"             — default import
//	import * as X from "pkg"        — namespace import
//	import { X } from "pkg"         — named import
//	export { X } from "pkg"         — re-export
//	export * from "pkg"             — re-export all
//	const x = require("pkg")        — CommonJS require
//	const x = await import("pkg")   — dynamic import
var importRe = regexp.MustCompile(
	`(?:from\s+|import\s+|require\s*\(\s*|import\s*\(\s*)['"]([^'"]+)['"]`,
)

// supportedExts is the set of file extensions whose content will be scanned.
var supportedExts = map[string]bool{
	".ts": true, ".tsx": true,
	".js": true, ".jsx": true,
	".mjs": true, ".cjs": true,
}

// Scanner walks source directories and extracts third-party package names.
// The zero value is ready to use; construct one with New() for clarity.
type Scanner struct{}

// New returns a ready-to-use Scanner.
func New() *Scanner { return &Scanner{} }

// ScanDir walks srcDir recursively and returns every unique third-party package
// name found across all supported source files.
//
// node_modules is always skipped — scanning it would produce thousands of false
// positives from vendored dependencies.  Unreadable files are skipped with a
// warning rather than aborting the whole walk.
func (s *Scanner) ScanDir(srcDir string) (map[string]bool, error) {
	found := make(map[string]bool)

	err := filepath.WalkDir(srcDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		// Skip the entire node_modules subtree — never scan vendored code.
		if d.IsDir() && d.Name() == "node_modules" {
			return filepath.SkipDir
		}
		if d.IsDir() || !supportedExts[filepath.Ext(path)] {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			// An unreadable file (e.g. wrong permissions) should not abort
			// the entire scan — skip it and continue.
			return nil
		}
		defer f.Close()

		for _, pkg := range s.ExtractImports(f) {
			found[pkg] = true
		}
		return nil
	})

	return found, err
}

// ExtractImports reads source content from r and returns the deduplicated set
// of third-party package names it imports.
//
// It is exported so that callers can test extraction against an in-memory
// string without touching the filesystem.
func (s *Scanner) ExtractImports(r io.Reader) []string {
	seen := make(map[string]bool)
	sc := bufio.NewScanner(r)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())

		// Skip single-line comments and JSDoc continuation lines so that
		// commented-out imports don't end up in the install list.
		if strings.HasPrefix(line, "//") || strings.HasPrefix(line, "*") {
			continue
		}

		for _, m := range importRe.FindAllStringSubmatch(line, -1) {
			if pkg := packageName(m[1]); pkg != "" {
				seen[pkg] = true
			}
		}
	}

	result := make([]string, 0, len(seen))
	for p := range seen {
		result = append(result, p)
	}
	return result
}

// packageName converts a raw module specifier (the string inside the quotes)
// into an installable package name, returning "" for specifiers that should be
// ignored entirely.
//
// Rules:
//   - Relative paths ("./foo", "../bar") → ignored
//   - Absolute paths ("/foo") → ignored
//   - Path aliases ("@/foo", "~/foo") → ignored
//   - Node built-ins ("fs", "path", …) → ignored
//   - Scoped packages ("@scope/pkg/sub") → "@scope/pkg"
//   - Standard packages ("pkg/sub") → "pkg"
func packageName(specifier string) string {
	// Relative and absolute paths are never installable.
	if strings.HasPrefix(specifier, ".") || strings.HasPrefix(specifier, "/") {
		return ""
	}

	// Path aliases used by bundlers/TypeScript (e.g. "@/components", "~/utils").
	// These look like scoped packages but the character after "@" or "~" is
	// immediately a "/" — there is no real npm scope here.
	if strings.HasPrefix(specifier, "@/") || strings.HasPrefix(specifier, "~/") {
		return ""
	}

	// Scoped package: keep only the first two path segments (@scope/pkg).
	if strings.HasPrefix(specifier, "@") {
		parts := strings.SplitN(specifier, "/", 3)
		if len(parts) >= 2 {
			return parts[0] + "/" + parts[1]
		}
		return specifier
	}

	// Standard package: the root segment before any sub-path.
	root := strings.SplitN(specifier, "/", 2)[0]
	if nodeBuiltins[root] {
		return ""
	}
	return root
}
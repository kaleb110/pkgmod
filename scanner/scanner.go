// Package scanner walks a JavaScript/TypeScript source tree and extracts the
// set of third-party package names referenced by import and require statements.
//
// It is intentionally decoupled from the rest of the tool: it knows nothing
// about package.json, installers, or the CLI.  This makes it independently
// testable and reusable.
package scanner

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

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
// Construct one with New() rather than using the zero value, so that options
// such as custom exclusion prefixes are applied correctly.
type Scanner struct {
	// excludePrefixes holds additional specifier prefixes the user wants to
	// treat as non-installable (e.g. "~", "$", "virtual:").  Checked after
	// the built-in rules so it only needs to cover project-specific aliases.
	excludePrefixes []string
}

// New returns a Scanner configured with the given exclusion prefixes.
// Pass nil or an empty slice when no extra exclusions are needed.
//
// Example — exclude Vite virtual modules and a custom alias:
//
//	sc := scanner.New([]string{"virtual:", "$app/"})
func New(excludePrefixes []string) *Scanner {
	return &Scanner{excludePrefixes: excludePrefixes}
}

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
			if pkg := s.packageName(m[1]); pkg != "" {
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
// Built-in detection uses the "node:" prefix rather than a static allowlist —
// this is the approach recommended by Node.js itself and automatically covers
// any built-in added in future Node versions without requiring code changes.
// Bare names like `import fs from "fs"` are still common in older code and
// are handled by also checking the legacy bare-name form.
//
// Rules (evaluated in order):
//  1. Relative paths ("./foo", "../bar")           → ignored
//  2. Absolute paths ("/foo")                       → ignored
//  3. Path aliases ("@/foo", "~/foo")               → ignored
//  4. Node built-ins ("node:fs", "node:path", …)   → ignored  (preferred form)
//  5. Node built-ins bare ("fs", "path", …)         → ignored  (legacy form)
//  6. User-supplied --exclude prefixes              → ignored
//  7. Scoped packages ("@scope/pkg/sub")            → "@scope/pkg"
//  8. Standard packages ("pkg/sub")                 → "pkg"
func (s *Scanner) packageName(specifier string) string {
	// 1 & 2. Relative and absolute paths are never installable.
	if strings.HasPrefix(specifier, ".") || strings.HasPrefix(specifier, "/") {
		return ""
	}

	// 3. Path aliases used by bundlers/TypeScript (e.g. "@/components",
	// "~/utils").  These look like scoped packages but the character after
	// "@" or "~" is immediately a "/" — there is no real npm scope here.
	if strings.HasPrefix(specifier, "@/") || strings.HasPrefix(specifier, "~/") {
		return ""
	}

	// 4. Node built-ins using the modern "node:" protocol prefix.
	// This is the canonical form recommended by Node.js (see
	// https://nodejs.org/api/esm.html#node-imports) and covers all current
	// and future built-ins without maintaining a static list.
	if strings.HasPrefix(specifier, "node:") {
		return ""
	}

	// 5. Legacy bare built-in names (e.g. "fs", "path") that predate the
	// "node:" prefix.  We keep a minimal list of the most commonly imported
	// ones so that old code doesn't generate false positives, while avoiding
	// the maintenance burden of a complete, ever-growing allowlist.
	//
	// Projects that have fully migrated to "node:" imports won't hit this
	// branch at all — the check above handles them.
	switch specifier {
	case "assert", "buffer", "child_process", "cluster", "console",
		"crypto", "dgram", "dns", "domain", "events", "fs", "http",
		"http2", "https", "module", "net", "os", "path", "perf_hooks",
		"process", "punycode", "querystring", "readline", "repl",
		"stream", "string_decoder", "timers", "tls", "tty", "url",
		"util", "v8", "vm", "worker_threads", "zlib":
		return ""
	}

	// 6. User-supplied exclusion prefixes (from --exclude flag).
	// Checked after the built-in rules so that the flag only needs to cover
	// project-specific aliases, not Node internals.
	for _, prefix := range s.excludePrefixes {
		if prefix != "" && strings.HasPrefix(specifier, prefix) {
			return ""
		}
	}

	// 7. Scoped package: keep only the first two path segments (@scope/pkg).
	if strings.HasPrefix(specifier, "@") {
		parts := strings.SplitN(specifier, "/", 3)
		if len(parts) >= 2 {
			return parts[0] + "/" + parts[1]
		}
		return specifier
	}

	// 8. Standard package: the root segment before any sub-path.
	return strings.SplitN(specifier, "/", 2)[0]
}
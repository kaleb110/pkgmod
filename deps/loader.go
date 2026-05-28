// Package deps handles everything related to the project's declared
// dependencies: reading and parsing package.json, normalising package name keys
// (which pnpm sometimes writes with an "@version" suffix), and computing the
// diff between what the source code imports and what is already declared.
//
// This package contains no I/O beyond reading a single file, which makes the
// core comparison logic (Diff) fully testable with plain in-memory maps.
package deps

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// packageJSON mirrors the four dependency maps we care about in a package.json
// file.  All four are checked during comparison so that packages declared in
// devDependencies, peerDependencies, or optionalDependencies are not
// incorrectly flagged as missing.
type packageJSON struct {
	Dependencies         map[string]string `json:"dependencies"`
	DevDependencies      map[string]string `json:"devDependencies"`
	PeerDependencies     map[string]string `json:"peerDependencies"`
	OptionalDependencies map[string]string `json:"optionalDependencies"`
}

// Load reads the package.json at path and returns a flat set of every package
// name declared across all four dependency maps.
//
// If the file does not exist, an empty set is returned without error — callers
// can treat a missing package.json as "nothing declared yet".  Any other read
// or parse error is returned to the caller.
func Load(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]bool{}, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	declared := make(map[string]bool)
	for _, m := range []map[string]string{
		pkg.Dependencies,
		pkg.DevDependencies,
		pkg.PeerDependencies,
		pkg.OptionalDependencies,
	} {
		for k := range m {
			// Normalise before storing so lookup always works regardless of
			// whether the key was written as "pkg" or "pkg@1.2.3".
			declared[normalizeKey(k)] = true
		}
	}
	return declared, nil
}

// Diff returns the packages that appear in found but are absent from declared,
// sorted alphabetically for stable output.
//
// found is the set returned by scanner.ScanDir; declared is the set returned
// by Load.  Keeping them as separate map[string]bool values means Diff is a
// pure function with no I/O, making it trivially unit-testable.
func Diff(found, declared map[string]bool) []string {
	var missing []string
	for pkg := range found {
		if !declared[pkg] {
			missing = append(missing, pkg)
		}
	}
	sort.Strings(missing)
	return missing
}

// normalizeKey strips a pnpm-style version suffix from a dependency key so
// that the stored name always matches what an import statement would use.
//
// Examples:
//
//	"react@^18.0.0"            → "react"
//	"zustand@^5.0.13"          → "zustand"
//	"@tanstack/react-query@^5" → "@tanstack/react-query"
//	"@scope/pkg"               → "@scope/pkg"   (no version — unchanged)
func normalizeKey(k string) string {
	if strings.HasPrefix(k, "@") {
		// Scoped package: the leading "@" is part of the name, so we look for
		// a second "@" that would mark the start of the version.
		rest := k[1:] // drop the leading "@"
		if idx := strings.Index(rest, "@"); idx >= 0 {
			return "@" + rest[:idx]
		}
		return k // no version suffix — return as-is
	}

	// Unscoped package: the first "@" is the version separator.
	if idx := strings.Index(k, "@"); idx >= 0 {
		return k[:idx]
	}
	return k
}
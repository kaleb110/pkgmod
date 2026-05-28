// Package deps handles everything related to the project's declared
// dependencies: reading and parsing package.json, normalising package name keys
// (which pnpm sometimes writes with an "@version" suffix), computing the diff
// between what the source code imports and what is already declared, and
// resolving which package manager to use.
//
// The core comparison logic (Diff) is a pure function with no I/O, making it
// trivially unit-testable with plain in-memory maps.
package deps

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// File is the parsed representation of a package.json that callers work with.
// It is returned by LoadFile and carries both the dependency sets and the
// optional packageManager field so that cmd can make a single read call and
// get everything it needs.
type File struct {
	// PackageManager is the raw value of the "packageManager" key, e.g.
	// "pnpm@9.1.0".  It is empty when the key is absent.
	PackageManager string

	// Declared is the flat set of every package name across all four
	// dependency maps (dependencies, devDependencies, peerDependencies,
	// optionalDependencies).
	Declared map[string]bool
}

// packageJSON mirrors the fields we care about in a package.json file.
type packageJSON struct {
	PackageManager       string            `json:"packageManager"`
	Dependencies         map[string]string `json:"dependencies"`
	DevDependencies      map[string]string `json:"devDependencies"`
	PeerDependencies     map[string]string `json:"peerDependencies"`
	OptionalDependencies map[string]string `json:"optionalDependencies"`
}

// LoadFile reads the package.json at path and returns a File.
//
// Unlike the old Load function, a missing file is now a hard error — the
// caller (cmd.Run) is responsible for checking existence first and surfacing
// a clear message to the user.
func LoadFile(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var raw packageJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	declared := make(map[string]bool)
	for _, m := range []map[string]string{
		raw.Dependencies,
		raw.DevDependencies,
		raw.PeerDependencies,
		raw.OptionalDependencies,
	} {
		for k := range m {
			// Normalise before storing so lookup always works regardless of
			// whether the key was written as "pkg" or "pkg@1.2.3".
			declared[normalizeKey(k)] = true
		}
	}

	return &File{
		PackageManager: raw.PackageManager,
		Declared:       declared,
	}, nil
}

// ResolveManager determines which package manager to use following a strict
// priority order:
//
//  1. packageManager field in package.json (e.g. "pnpm@9.1.0")
//     — if present, it must match a supported manager or an error is returned.
//  2. --manager flag value passed by the user.
//     — used when packageManager is absent from package.json.
//
// The name extracted from the field (the part before "@") is passed to
// parseFn so that the installer package remains the single source of truth
// for what is supported.  parseFn is installer.ParseManager in production and
// a stub in tests.
func ResolveManager(pkgManagerField, flagValue string, parseFn func(string) (string, error)) (string, error) {
	if pkgManagerField != "" {
		// Extract just the name: "pnpm@9.1.0" → "pnpm"
		name := strings.SplitN(pkgManagerField, "@", 2)[0]
		resolved, err := parseFn(name)
		if err != nil {
			// Surface the unsupported-manager error with extra context so the
			// user knows it came from package.json, not their flag.
			return "", fmt.Errorf("package.json packageManager %q: %w", pkgManagerField, err)
		}
		return resolved, nil
	}

	// No packageManager field — fall back to the --manager flag.
	return parseFn(flagValue)
}

// Diff returns the packages that appear in found but are absent from declared,
// sorted alphabetically for stable output.
//
// found is the set returned by scanner.ScanDir; declared comes from File.Declared.
// Keeping this as a pure function means it is testable with two plain maps.
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
		rest := k[1:]
		if idx := strings.Index(rest, "@"); idx >= 0 {
			return "@" + rest[:idx]
		}
		return k
	}
	if idx := strings.Index(k, "@"); idx >= 0 {
		return k[:idx]
	}
	return k
}
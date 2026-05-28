package deps_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/kaleb110/pkgmod/deps"
)

func sorted(s []string) []string {
	out := make([]string, len(s))
	copy(out, s)
	sort.Strings(out)
	return out
}

func tmpDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "depsyncer-deps-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

// ── LoadFile ──────────────────────────────────────────────────────────────────

// TestLoadFile_AllFourMaps verifies that packages across all four dependency
// maps are included in File.Declared.
func TestLoadFile_AllFourMaps(t *testing.T) {
	dir := tmpDir(t)
	writeFile(t, dir, "package.json", `{
		"dependencies":         {"react":      "^18"},
		"devDependencies":      {"typescript": "^5"},
		"peerDependencies":     {"react-dom":  "*"},
		"optionalDependencies": {"fsevents":   "^2"}
	}`)

	f, err := deps.LoadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"react", "typescript", "react-dom", "fsevents"} {
		if !f.Declared[want] {
			t.Errorf("expected %q in declared deps", want)
		}
	}
}

// TestLoadFile_ReadsPackageManagerField verifies that the packageManager key
// is exposed on File so cmd can use it for manager resolution.
func TestLoadFile_ReadsPackageManagerField(t *testing.T) {
	dir := tmpDir(t)
	writeFile(t, dir, "package.json", `{
		"packageManager": "pnpm@9.1.0",
		"dependencies": {"react": "^18"}
	}`)

	f, err := deps.LoadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if f.PackageManager != "pnpm@9.1.0" {
		t.Errorf("got PackageManager=%q, want %q", f.PackageManager, "pnpm@9.1.0")
	}
}

// TestLoadFile_AbsentPackageManagerField verifies that File.PackageManager is
// empty when the key is absent — this is the signal to fall back to the flag.
func TestLoadFile_AbsentPackageManagerField(t *testing.T) {
	dir := tmpDir(t)
	writeFile(t, dir, "package.json", `{"dependencies":{"react":"^18"}}`)

	f, err := deps.LoadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if f.PackageManager != "" {
		t.Errorf("expected empty PackageManager, got %q", f.PackageManager)
	}
}

// TestLoadFile_MissingFile verifies that a missing package.json is now a hard
// error — cmd.Run is responsible for the existence check and user message.
func TestLoadFile_MissingFile(t *testing.T) {
	_, err := deps.LoadFile("/no/such/package.json")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

// TestLoadFile_MalformedJSON verifies that a JSON parse failure is returned
// as an error.
func TestLoadFile_MalformedJSON(t *testing.T) {
	dir := tmpDir(t)
	writeFile(t, dir, "package.json", `{not valid json`)
	_, err := deps.LoadFile(filepath.Join(dir, "package.json"))
	if err == nil {
		t.Error("expected parse error, got nil")
	}
}

// TestLoadFile_PnpmVersionedKeys is the regression test for pnpm writing dep
// keys as "pkg@version" instead of just "pkg".
func TestLoadFile_PnpmVersionedKeys(t *testing.T) {
	dir := tmpDir(t)
	writeFile(t, dir, "package.json", `{
		"packageManager": "pnpm@9.1.0",
		"dependencies":   {"zustand": "^5.0.13"}
	}`)

	f, err := deps.LoadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !f.Declared["zustand"] {
		t.Errorf("zustand should be in declared deps; got %v", f.Declared)
	}
}

// ── ResolveManager ────────────────────────────────────────────────────────────

// parseFn is a test stub that accepts only "pnpm", "bun", "npm", "yarn".
func parseFn(s string) (string, error) {
	supported := map[string]bool{"pnpm": true, "bun": true, "npm": true, "yarn": true}
	if !supported[s] {
		return "", fmt.Errorf("unsupported package manager %q", s)
	}
	return s, nil
}

// TestResolveManager_PkgJsonFieldTakesPriority verifies that when packageManager
// is present in package.json it wins over the --manager flag.
func TestResolveManager_PkgJsonFieldTakesPriority(t *testing.T) {
	got, err := deps.ResolveManager("pnpm@9.1.0", "bun", parseFn)
	if err != nil {
		t.Fatal(err)
	}
	if got != "pnpm" {
		t.Errorf("got %q, want %q", got, "pnpm")
	}
}

// TestResolveManager_FlagUsedWhenFieldAbsent verifies that when packageManager
// is absent the --manager flag is used.
func TestResolveManager_FlagUsedWhenFieldAbsent(t *testing.T) {
	got, err := deps.ResolveManager("", "bun", parseFn)
	if err != nil {
		t.Fatal(err)
	}
	if got != "bun" {
		t.Errorf("got %q, want %q", got, "bun")
	}
}

// TestResolveManager_UnsupportedFieldReturnsError verifies that an unrecognised
// value in the packageManager field is rejected with an error — the user should
// update the field or use a supported manager, not silently fall back to the flag.
func TestResolveManager_UnsupportedFieldReturnsError(t *testing.T) {
	_, err := deps.ResolveManager("yarn-berry@4.0.0", "pnpm", parseFn)
	if err == nil {
		t.Error("expected error for unsupported manager in packageManager field")
	}
}

// TestResolveManager_UnsupportedFlagReturnsError verifies that an unrecognised
// --manager flag value is also rejected.
func TestResolveManager_UnsupportedFlagReturnsError(t *testing.T) {
	_, err := deps.ResolveManager("", "cargo", parseFn)
	if err == nil {
		t.Error("expected error for unsupported --manager flag value")
	}
}

// TestResolveManager_StripsVersionFromField verifies that "pnpm@9.1.0" is
// correctly reduced to "pnpm" before being passed to parseFn.
func TestResolveManager_StripsVersionFromField(t *testing.T) {
	for _, field := range []string{"pnpm@9.1.0", "bun@1.0.0", "npm@10.2.3", "yarn@3.6.0"} {
		t.Run(field, func(t *testing.T) {
			_, err := deps.ResolveManager(field, "pnpm", parseFn)
			if err != nil {
				t.Errorf("unexpected error for %q: %v", field, err)
			}
		})
	}
}

// ── Diff ──────────────────────────────────────────────────────────────────────

func TestDiff(t *testing.T) {
	cases := []struct {
		name     string
		found    map[string]bool
		declared map[string]bool
		want     []string
	}{
		{
			name:     "nothing missing",
			found:    map[string]bool{"react": true, "axios": true},
			declared: map[string]bool{"react": true, "axios": true},
			want:     []string{},
		},
		{
			name:     "all missing",
			found:    map[string]bool{"react": true, "axios": true},
			declared: map[string]bool{},
			want:     []string{"axios", "react"},
		},
		{
			name:     "partial overlap",
			found:    map[string]bool{"react": true, "axios": true, "lodash": true},
			declared: map[string]bool{"react": true},
			want:     []string{"axios", "lodash"},
		},
		{
			name:     "empty found — nothing to install",
			found:    map[string]bool{},
			declared: map[string]bool{"react": true},
			want:     []string{},
		},
		{
			name:     "declared superset of found — no false positives",
			found:    map[string]bool{"react": true},
			declared: map[string]bool{"react": true, "lodash": true, "axios": true},
			want:     []string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := deps.Diff(tc.found, tc.declared)
			want := sorted(tc.want)
			if fmt.Sprint(got) != fmt.Sprint(want) {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
}

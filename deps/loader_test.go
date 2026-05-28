package deps_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/kaleb110/deps"
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

// ── Load ──────────────────────────────────────────────────────────────────────

// TestLoad_AllFourMaps verifies that packages declared across all four
// dependency maps are included in the returned set.
func TestLoad_AllFourMaps(t *testing.T) {
	dir := tmpDir(t)
	writeFile(t, dir, "package.json", `{
		"dependencies":         {"react":    "^18"},
		"devDependencies":      {"typescript": "^5"},
		"peerDependencies":     {"react-dom": "*"},
		"optionalDependencies": {"fsevents":  "^2"}
	}`)

	got, err := deps.Load(filepath.Join(dir, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"react", "typescript", "react-dom", "fsevents"} {
		if !got[want] {
			t.Errorf("expected %q in declared deps", want)
		}
	}
}

// TestLoad_MissingFile verifies that a non-existent package.json is treated as
// "no dependencies declared" rather than an error.
func TestLoad_MissingFile(t *testing.T) {
	got, err := deps.Load("/no/such/package.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty set, got %v", got)
	}
}

// TestLoad_MalformedJSON verifies that a JSON parse failure is returned as an
// error, not silently swallowed.
func TestLoad_MalformedJSON(t *testing.T) {
	dir := tmpDir(t)
	writeFile(t, dir, "package.json", `{not valid json`)
	_, err := deps.Load(filepath.Join(dir, "package.json"))
	if err == nil {
		t.Error("expected parse error, got nil")
	}
}

// TestLoad_EmptyFile verifies that `{}` returns an empty set without error.
func TestLoad_EmptyFile(t *testing.T) {
	dir := tmpDir(t)
	writeFile(t, dir, "package.json", `{}`)
	got, err := deps.Load(filepath.Join(dir, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty set, got %v", got)
	}
}

// TestLoad_PnpmVersionedKeys is the regression test for the pnpm bug where
// dependency keys can be written as "pkg@version" instead of just "pkg".
func TestLoad_PnpmVersionedKeys(t *testing.T) {
	dir := tmpDir(t)

	// Reproduces the exact package.json from the original bug report.
	writeFile(t, dir, "package.json", `{
		"packageManager": "pnpm@1.2",
		"dependencies": {
			"zustand": "^5.0.13"
		}
	}`)

	got, err := deps.Load(filepath.Join(dir, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !got["zustand"] {
		t.Errorf("zustand should be in declared deps; got %v", got)
	}
}

// TestLoad_PnpmAtVersionedKeys verifies that keys written with an explicit
// "@version" suffix (a less common but valid pnpm format) are normalised.
func TestLoad_PnpmAtVersionedKeys(t *testing.T) {
	dir := tmpDir(t)
	writeFile(t, dir, "package.json", `{
		"dependencies": {
			"react@^18.0.0":              "*",
			"@tanstack/react-query@^5.0": "*"
		}
	}`)

	got, err := deps.Load(filepath.Join(dir, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"react", "@tanstack/react-query"} {
		if !got[want] {
			t.Errorf("expected %q after normalisation; got %v", want, got)
		}
	}
}

// ── Diff ──────────────────────────────────────────────────────────────────────

// TestDiff covers the full set of comparison outcomes: everything present,
// everything missing, partial overlap, and false-positive prevention when
// declared has more entries than found.
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
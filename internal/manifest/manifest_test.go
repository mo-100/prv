package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

// touch creates a file named name in dir.
func touch(t *testing.T, dir, name string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestTagsEmptyDir(t *testing.T) {
	dir := t.TempDir()
	if got := Tags(dir, 1); len(got) != 0 {
		t.Fatalf("empty dir: want nil/empty, got %v", got)
	}
}

func TestTagsMissingDir(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if got := Tags(missing, 1); len(got) != 0 {
		t.Fatalf("missing dir: want nil/empty, got %v", got)
	}
}

func TestTagsNodeOnly(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "package.json")
	if got := Tags(dir, 1); len(got) != 1 || got[0] != "node" {
		t.Fatalf("node-only: want [node], got %v", got)
	}
}

func TestTagsGoThenNodeOrder(t *testing.T) {
	dir := t.TempDir()
	// Create in non-catalog order to prove we ignore filesystem order.
	touch(t, dir, "package.json")
	touch(t, dir, "go.mod")
	got := Tags(dir, 1)
	want := []string{"go", "node"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("go+node: want %v, got %v", want, got)
	}
}

func TestTagsNodeThenGoStillCatalogOrder(t *testing.T) {
	dir := t.TempDir()
	// Reverse creation order from the previous test; result must be identical.
	touch(t, dir, "go.mod")
	touch(t, dir, "package.json")
	got := Tags(dir, 1)
	want := []string{"go", "node"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("go+node (rev): want %v, got %v", want, got)
	}
}

func TestTagsCsprojSuffix(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "My.App.csproj")
	got := Tags(dir, 1)
	if len(got) != 1 || got[0] != "csharp" {
		t.Fatalf("csproj: want [csharp], got %v", got)
	}
}

func TestTagsFullCatalogOrder(t *testing.T) {
	dir := t.TempDir()
	// One file per ecosystem, created in reverse catalog order.
	touch(t, dir, "mix.exs")
	touch(t, dir, "Gemfile")
	touch(t, dir, "a.csproj")
	touch(t, dir, "build.gradle")
	touch(t, dir, "requirements.txt")
	touch(t, dir, "package.json")
	touch(t, dir, "Cargo.toml")
	touch(t, dir, "go.mod")

	want := []string{"go", "rust", "node", "python", "java", "csharp", "ruby", "elixir"}
	got := Tags(dir, 1)
	if len(got) != len(want) {
		t.Fatalf("full catalog: want %d tags, got %d (%v)", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("full catalog order: want %v, got %v", want, got)
		}
	}
}

func TestTagsPythonDeDupesAcrossSources(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "pyproject.toml")
	touch(t, dir, "setup.py")
	touch(t, dir, "requirements.txt")
	got := Tags(dir, 1)
	// Multiple python manifests collapse to a single "python" tag.
	if len(got) != 1 || got[0] != "python" {
		t.Fatalf("python dedupe: want [python], got %v", got)
	}
}

func TestTagsIgnoresNonManifestFiles(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "README.md")
	touch(t, dir, "src.txt")
	if got := Tags(dir, 1); len(got) != 0 {
		t.Fatalf("non-manifest: want empty, got %v", got)
	}
}

// Depth recursion: depth=1 stops at dir; depth=2 also scans subdirectories.
func TestTagsDepth1IgnoresSubdir(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "go.mod")
	backend := filepath.Join(dir, "backend")
	if err := os.Mkdir(backend, 0o755); err != nil {
		t.Fatal(err)
	}
	touch(t, backend, "package.json")
	// depth=1 sees only go; the node manifest in backend/ is invisible.
	got := Tags(dir, 1)
	if len(got) != 1 || got[0] != "go" {
		t.Fatalf("depth=1: want [go], got %v", got)
	}
}

// A manifest at depth 1 must NOT suppress one at depth 2 (the union is taken
// across all levels). This is the regression guard for the old
// "len(tags)==0 gate" bug.
func TestTagsDepth2UnionsRootAndSubdir(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "go.mod") // depth 1
	backend := filepath.Join(dir, "backend")
	if err := os.Mkdir(backend, 0o755); err != nil {
		t.Fatal(err)
	}
	touch(t, backend, "package.json") // depth 2
	got := Tags(dir, 2)
	want := []string{"go", "node"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("depth=2 union: want %v, got %v", want, got)
	}
}

// Two manifests at disjoint depths both contribute (catalog order preserved).
func TestTagsDeepUnionsAcrossHeights(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "go.mod") // depth 1
	mid := filepath.Join(dir, "services", "api")
	if err := os.MkdirAll(mid, 0o755); err != nil {
		t.Fatal(err)
	}
	touch(t, mid, "Cargo.toml") // depth 3
	// depth=3 reaches it; depth=2 does not.
	if got := Tags(dir, 2); len(got) != 1 || got[0] != "go" {
		t.Fatalf("depth=2: want [go], got %v", got)
	}
	got := Tags(dir, 3)
	want := []string{"go", "rust"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("depth=3: want %v, got %v", want, got)
	}
}

// The shared skip list prunes dependency/vendored trees so they never inject
// false tags. A python project that also vendored a JS tool's node_modules
// must not report "node".
func TestTagsPrunesSkipDirs(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "pyproject.toml")
	nm := filepath.Join(dir, "node_modules", "some-pkg")
	if err := os.MkdirAll(nm, 0o755); err != nil {
		t.Fatal(err)
	}
	touch(t, nm, "package.json") // would falsely add "node" if not pruned
	got := Tags(dir, 2)
	if len(got) != 1 || got[0] != "python" {
		t.Fatalf("skip prune: want [python], got %v", got)
	}
}

// Hidden directories are not descended into during the manifest walk.
func TestTagsPrunesHiddenDirs(t *testing.T) {
	dir := t.TempDir()
	vscode := filepath.Join(dir, ".vscode")
	if err := os.Mkdir(vscode, 0o755); err != nil {
		t.Fatal(err)
	}
	touch(t, vscode, "package.json")
	if got := Tags(dir, 2); len(got) != 0 {
		t.Fatalf("hidden prune: want empty, got %v", got)
	}
}

// A catalog tag dedupes across depths too: the same ecosystem at root and in a
// child fires only one slot.
func TestTagsDedupesAcrossDepths(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "go.mod")
	sub := filepath.Join(dir, "cmd", "tool")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	touch(t, sub, "go.mod")
	got := Tags(dir, 3)
	if len(got) != 1 || got[0] != "go" {
		t.Fatalf("cross-depth dedupe: want [go], got %v", got)
	}
}

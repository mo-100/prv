package project

import (
	"os"
	"path/filepath"
	"testing"
)

// The catalog collapse made Dockerfile and docker-compose.yml first-class
// project signals (not just tag sources). These guard that contract — a
// folder carrying only one of them classifies as a project where it
// previously did not. If the catalog ever drops one, these catch it.
func TestHasSignalDockerManifests(t *testing.T) {
	for _, name := range []string{"Dockerfile", "docker-compose.yml", "go.mod", "package.json", "Foo.csproj"} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatalf("touch %s: %v", name, err)
		}
		if !HasSignal(dir) {
			t.Errorf("HasSignal(%q) = false; want true (catalog entry no longer counts as a signal)", name)
		}
	}
}

func TestIsManifest(t *testing.T) {
	cases := map[string]bool{
		"go.mod":             true,
		"Dockerfile":         true,
		"docker-compose.yml": true,
		"a.csproj":           true,
		"README.md":          false,
		"deploy":             false,
		"":                   false,
	}
	for name, want := range cases {
		if got := IsManifest(name); got != want {
			t.Errorf("IsManifest(%q) = %v; want %v", name, got, want)
		}
	}
}

// TestHasSignalTODOFiles locks the contract that a directory is a project when
// it holds a recognized TODO file, and only those. The set is delegated to
// todolist.IsTODOName (the single source of truth), so detection and the TODO
// render can never disagree. Bare TODO is a documented signal; TODO.org and
// other TODO.* are not (the old isSignalName prefix over-accepted them while
// the render ignored them — a live bug).
func TestHasSignalTODOFiles(t *testing.T) {
	yes := []string{"TODO", "TODO.md", "TODO.txt", "Todo.md", "todo.TXT"}
	for _, name := range yes {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatalf("touch %s: %v", name, err)
		}
		if !HasSignal(dir) {
			t.Errorf("HasSignal(%q) = false; want true (in todolist.IsTODOName)", name)
		}
	}
	no := []string{"TODO.org", "TODO.bak", "TODO.swp", "README.md", "tasks.md"}
	for _, name := range no {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatalf("touch %s: %v", name, err)
		}
		if HasSignal(dir) {
			t.Errorf("HasSignal(%q) = true; want false (not in todolist.IsTODOName)", name)
		}
	}
}

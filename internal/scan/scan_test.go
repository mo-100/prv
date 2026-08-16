package scan

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mo-100/prv/internal/project"
)

// write creates path (and its parents) with the given content. Empty content
// creates an empty marker file (e.g. `.prv`).
func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// rows returns the projects Run(root, cfg) yields.
func rows(t *testing.T, root string, cfg Config) []project.Project {
	t.Helper()
	return Run(root, cfg)
}

// byName finds the row whose Name matches, failing the test otherwise.
func byName(t *testing.T, rs []project.Project, name string) *project.Project {
	t.Helper()
	for i := range rs {
		if rs[i].Name == name {
			return &rs[i]
		}
	}
	t.Fatalf("no row named %q; got rows: %v", name, rs)
	return nil
}

// TestRunSeedRollupAtDefaultDepth is the flagship "what happens?" scenario: a
// project row with its own manifest absorbs nested different-ecosystem subdir
// manifests into a single row with the union of tags (in catalog order), and
// the nested manifest does not surface as its own row (project atomicity).
func TestRunSeedRollupAtDefaultDepth(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "X/requirements.txt"), "")
	write(t, filepath.Join(root, "X/node/package.json"), "{}")

	rs := rows(t, root, NewConfig())
	if len(rs) != 1 {
		t.Fatalf("want exactly 1 row, got %d: %v", len(rs), rs)
	}
	got := rs[0]
	if got.Name != "X" {
		t.Errorf("Name = %q, want %q", got.Name, "X")
	}
	if got.Kind != project.KindProject {
		t.Errorf("Kind = %v, want KindProject", got.Kind)
	}
	wantTags := []string{"node", "python"} // catalog order, node precedes python
	if !reflect.DeepEqual(got.Tags, wantTags) {
		t.Errorf("Tags = %v, want %v (catalog order)", got.Tags, wantTags)
	}
}

// TestRunSeedAtDepth1: with a depth of 1 the seed row's manifest/TODO budget is
// max(1,1-1+1)=1, so only the row's own directory is searched — the nested
// node manifest at row-depth 2 is beyond budget and contributes no tag.
func TestRunSeedAtDepth1(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "X/requirements.txt"), "")
	write(t, filepath.Join(root, "X/node/package.json"), "{}")

	rs := rows(t, root, Config{Depth: 1})
	if len(rs) != 1 {
		t.Fatalf("want exactly 1 row, got %d: %v", len(rs), rs)
	}
	got := rs[0]
	if got.Name != "X" {
		t.Errorf("Name = %q, want %q", got.Name, "X")
	}
	wantTags := []string{"python"} // node manifest at row-depth 2 is beyond budget 1
	if !reflect.DeepEqual(got.Tags, wantTags) {
		t.Errorf("Tags = %v, want %v", got.Tags, wantTags)
	}
}

// TestRunContainerChildrenNoRollup: two sibling projects under a container each
// keep their own tags (per-row isolation) — no `work` container row, no row
// carrying both tags.
func TestRunContainerChildrenNoRollup(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "work/api/go.mod"), "module api")
	write(t, filepath.Join(root, "work/web/package.json"), "{}")

	rs := rows(t, root, NewConfig())

	api := byName(t, rs, "work/api")
	if api.Tags[0] != "go" || len(api.Tags) != 1 {
		t.Errorf("work/api Tags = %v, want [go]", api.Tags)
	}
	web := byName(t, rs, "work/web")
	if web.Tags[0] != "node" || len(web.Tags) != 1 {
		t.Errorf("work/web Tags = %v, want [node]", web.Tags)
	}

	if len(rs) != 2 {
		t.Fatalf("want exactly 2 rows, got %d: %v", len(rs), rs)
	}
	for _, r := range rs {
		if r.Name == "work" {
			t.Errorf("container must not produce a row, but got %q", r.Name)
		}
		if len(r.Tags) != 1 {
			t.Errorf("row %q carries both tags; want per-row isolation: %v", r.Name, r.Tags)
		}
	}
}

// TestRunRootWithOwnSignalSingleRow: a root with its own signal is a single
// project row (k=1) whose tag search rolls up child manifests across depths
// into one row — the regression that a child manifest suppresses the root.
func TestRunRootWithOwnSignalSingleRow(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "go.mod"), "module root")
	write(t, filepath.Join(root, "backend/package.json"), "{}")

	rs := rows(t, root, NewConfig())
	if len(rs) != 1 {
		t.Fatalf("want exactly 1 row, got %d: %v", len(rs), rs)
	}
	got := rs[0]
	if got.Name != filepath.Base(root) {
		t.Errorf("Name = %q, want %q", got.Name, filepath.Base(root))
	}
	if got.Kind != project.KindProject {
		t.Errorf("Kind = %v, want KindProject", got.Kind)
	}
	wantTags := []string{"go", "node"} // cross-depth union inside the single root row
	if !reflect.DeepEqual(got.Tags, wantTags) {
		t.Errorf("Tags = %v, want %v", got.Tags, wantTags)
	}
}

// TestRunHiddenDirWithSignalNotShown: hidden/skip wins over signals — a hidden
// dir carrying a project signal is never a row, and a container whose only
// signalled child is a skip-listed dir does not surface either.
func TestRunHiddenDirWithSignalNotShown(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, ".hidden/go.mod"), "module hidden")
	write(t, filepath.Join(root, "app/go.mod"), "module app")
	write(t, filepath.Join(root, "box/node_modules/pkg/package.json"), "{}")

	rs := rows(t, root, NewConfig())
	for _, r := range rs {
		if r.Name == ".hidden" {
			t.Errorf("hidden dir with a signal must produce no row, but got %q", r.Name)
		}
		if r.Name == "box" {
			t.Errorf("container whose only signalled child is a skip dir must produce no row, but got %q", r.Name)
		}
	}

	app := byName(t, rs, "app")
	if app.Kind != project.KindProject || app.Tags[0] != "go" {
		t.Errorf("app row = %+v, want KindProject with [go]", app)
	}
	if len(rs) != 1 {
		t.Fatalf("want exactly 1 row (app), got %d: %v", len(rs), rs)
	}
}

// TestRunContainerAtDepthCapNoRow: a container at the depth cap produces no
// row rather than expanding, and its signalled children are too deep to
// surface, so the run yields an empty slice.
func TestRunContainerAtDepthCapNoRow(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "work/api/go.mod"), "module api")

	rs := rows(t, root, Config{Depth: 1})
	if len(rs) != 0 {
		t.Fatalf("want no rows (container at depth cap), got %d: %v", len(rs), rs)
	}
}

// TestRunTODOPrunedFromSkipDir: a TODO.md inside a skip-listed subdirectory
// (node_modules) is pruned during the todolist walk, so the row reports no
// TODO even though the default-depth budget (4) would otherwise reach it.
func TestRunTODOPrunedFromSkipDir(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "app/go.mod"), "module app")
	write(t, filepath.Join(root, "app/node_modules/lib/TODO.md"), "- [ ] x")

	rs := rows(t, root, NewConfig())
	app := byName(t, rs, "app")
	if app.TODO {
		t.Errorf("TODO = true, want false (TODO.md under node_modules must be pruned)")
	}
	if app.TODOOpen != 0 {
		t.Errorf("TODOOpen = %d, want 0", app.TODOOpen)
	}
}

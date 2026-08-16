package activity

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mo-100/prv/internal/git"
)

func TestForDirNewestMtime(t *testing.T) {
	dir := t.TempDir()
	older := time.Now().Add(-48 * time.Hour)
	newest := time.Now().Add(-1 * time.Hour)

	writeFile(t, filepath.Join(dir, "a.txt"), older)
	writeFile(t, filepath.Join(dir, "b.txt"), newest)
	// a skipped dir must not contribute even if its file is newest.
	skipDir := filepath.Join(dir, "node_modules")
	if err := os.Mkdir(skipDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(skipDir, "ignored.txt"), time.Now().Add(1*time.Hour))

	got := ForDir(dir)
	if got.IsZero() {
		t.Fatal("ForDir returned zero time for non-empty dir")
	}
	if got.Sub(newest) > time.Second {
		t.Fatalf("ForDir = %v, want ~%v (newest non-skipped mtime)", got, newest)
	}
}

func TestForDirRespectsDepthCap(t *testing.T) {
	dir := t.TempDir()
	deep := dir
	// Build 12 nested dirs (> walkDepthCap) with a fresh file at the bottom.
	for range 12 {
		deep = filepath.Join(deep, "d")
		if err := os.Mkdir(deep, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(deep, "deep.txt"), time.Now().Add(1*time.Hour))

	got := ForDir(dir)
	if !got.IsZero() {
		t.Fatalf("ForDir = %v, want zero (depth cap should have skipped the deep file)", got)
	}
}

func TestForGitMaxOfCommitAndDirty(t *testing.T) {
	dir := initRepo(t) // deterministic commit time (2020-01-01)

	// Commit time is the baseline; a dirty file newer than the commit must win.
	dirty := filepath.Join(dir, "dirty.txt")
	writeFile(t, dirty, time.Now().Add(1*time.Hour))

	st, err := git.Scan(dir)
	if err != nil {
		t.Fatalf("git.Scan: %v", err)
	}
	st.DirtyPaths = []string{"dirty.txt"} // ensure it's in dirty paths

	got := ForGit(dir, st)
	if got.IsZero() {
		t.Fatal("ForGit returned zero time for a committed repo with a dirty file")
	}
	fi, _ := os.Stat(dirty)
	if got.Sub(fi.ModTime()) > time.Second {
		t.Fatalf("ForGit = %v, want ~%v (dirty file mtime)", got, fi.ModTime())
	}
}

func TestForGitNoDirtyFallsBackToCommit(t *testing.T) {
	dir := initRepo(t)
	st := &git.State{} // no dirty paths
	got := ForGit(dir, st)
	if got.IsZero() {
		t.Fatal("ForGit should fall back to last commit time when there are no dirty paths")
	}
}

func TestForGitBareStateNoCommits(t *testing.T) {
	dir := t.TempDir()
	// No git repo, nil state: returns zero time.
	if got := ForGit(dir, nil); !got.IsZero() {
		t.Fatalf("ForGit(nil state, non-repo) = %v, want zero", got)
	}
}

// --- helpers ---

func writeFile(t *testing.T, path string, mtime time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

// initRepo creates a real git repo in a temp dir with one committed file, at a
// deterministic commit time so assertions are stable.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	env := append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_AUTHOR_DATE=2020-01-01T00:00:00",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t", "GIT_COMMITTER_DATE=2020-01-01T00:00:00",
	)
	run := func(args ...string) {
		if out, err := gitCmd(env, dir, args...); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	writeFile(t, filepath.Join(dir, "committed.txt"), time.Now())
	run("add", "-A")
	run("commit", "-m", "init")
	return dir
}

func gitCmd(env []string, dir string, args ...string) ([]byte, error) {
	full := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", full...)
	cmd.Env = env
	return cmd.CombinedOutput()
}

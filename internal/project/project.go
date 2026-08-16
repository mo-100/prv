// Package project defines the shared Project record and the cheap per-folder
// classifier used by the scanner. This is the contract every engine package
// builds against; see docs/project.md for the locked spec.
package project

import (
	"os"
	"strings"
	"time"

	"github.com/mo-100/prv/internal/todolist"
)

// Kind is the classifier's verdict for a directory relative to the scan root.
// It is scanner bookkeeping, not a rendered dimension.
type Kind int

const (
	KindProject   Kind = iota // has its own project signals; full-scan target
	KindContainer             // no signals, but has signalled children; expand (≤ --depth)
	KindRaw                   // no signals and no signalled children (leaf or skipped)
)

// Project is the shared record the scanner fills and both frontends render.
// Field semantics mirror docs/project.md exactly; do not add rendered
// dimensions without updating the spec first.
type Project struct {
	Name       string // path relative to scan root ("my-app", "work/my-app")
	Path       string // absolute path to the project directory
	Kind       Kind   // classifier result; not rendered
	Depth      int    // row's root-depth (1 = root child, 2 = under one container, …); scanner bookkeeping for the per-row search budget, never rendered
	IsGit      bool   // a `.git` entry exists in the folder
	Branch     string // "" if not git
	Modified   int    // tracked+modified+unmerged, from porcelain v2
	Untracked  int
	Ahead      int       // vs local upstream tracking ref; -1 = no upstream
	Behind     int       // -1 = no upstream
	TODO       bool      // a TODO file was found
	TODOOpen   int       // count of unchecked `- [ ]` items
	Tags       []string  // detected ecosystems in catalog order; empty = none
	LastActive time.Time // git: max(last commit, dirty-file mtime); non-git: newest non-skipped mtime
	Err        error     // per-project scan failure; row renders with an error marker
}

// SurfaceIsGit is true for manifest/TODO signals too (used by report styling).
func (p Project) HasSignals() bool { return p.Kind == KindProject }

// Entry is one row of the manifest catalog: a tag and the name predicate that
// fires it. Catalog order is the deterministic report order shared by both
// project detection (IsManifest) and tag detection (internal/manifest.Tags).
type Entry struct {
	Tag   string
	Match func(name string) bool
}

// Catalog is the single source of truth for the manifest→tag mapping, in the
// deterministic report order. Append here to add an ecosystem - project
// detection ("does this folder carry a manifest?") and tag detection ("which
// ecosystem tags fire?") both read this one list, so adding a manifest is a
// single edit rather than a sync of two lists. A manifest name also classifies
// its folder as a project, so e.g. a Dockerfile-only folder is a project.
var Catalog = []Entry{
	{"go", func(n string) bool { return n == "go.mod" }},
	{"compose", func(n string) bool { return n == "docker-compose.yml" }},
	{"docker", func(n string) bool { return n == "Dockerfile" }},
	{"rust", func(n string) bool { return n == "Cargo.toml" }},
	{"node", func(n string) bool { return n == "package.json" }},
	{"python", func(n string) bool {
		return n == "pyproject.toml" || n == "setup.py" || n == "requirements.txt"
	}},
	{"java", func(n string) bool {
		return n == "pom.xml" || n == "build.gradle"
	}},
	{"csharp", func(n string) bool { return strings.HasSuffix(n, ".csproj") }},
	{"ruby", func(n string) bool { return n == "Gemfile" }},
	{"elixir", func(n string) bool { return n == "mix.exs" }},
}

// IsManifest reports whether name matches any catalog entry (a manifest file).
// It is the classifier's "is this a manifest?" check and is shared with
// internal/manifest so detection and tagging agree on what counts.
func IsManifest(name string) bool {
	for _, e := range Catalog {
		if e.Match(name) {
			return true
		}
	}
	return false
}

// isSignalName reports whether a single entry name is a project signal.
func isSignalName(name string) bool {
	if name == ".git" || name == ".prv" {
		return true
	}
	if IsManifest(name) {
		return true
	}
	// TODO file: bare TODO, TODO.md, TODO.txt. todolist.IsTODOName is the
	// single source of truth for which filenames count, so project detection
	// and the TODO render never disagree.
	if todolist.IsTODOName(name) {
		return true
	}
	return false
}

// HasSignal reports whether the directory at path contains any project signal
// (file-existence only - no subprocess). A read failure reports false; the
// scanner records the error on the row.
func HasSignal(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if isSignalName(e.Name()) {
			return true
		}
	}
	return false
}

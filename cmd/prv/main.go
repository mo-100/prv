// Package main is the prv entry point: arg parsing and dispatch to the ls or
// tui frontend. Invocation (locked):
//
//	prv              → TUI on .
//	prv tui          → TUI on .
//	prv tui <path>   → TUI on <path>
//	prv <path>       → TUI on <path>
//	prv ls           → table on . to stdout
//	prv ls <path>    → table on <path> to stdout
//
// When prv (TUI) is invoked with stdout not a TTY, fall back to ls output.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mo-100/prv/internal/project"
	"github.com/mo-100/prv/internal/render"
	"github.com/mo-100/prv/internal/report"
	"github.com/mo-100/prv/internal/scan"
	"github.com/mo-100/prv/internal/tui"
)

// usage is the flag/usage banner. Its default values and the sort field list
// are sourced from the one-home defaults (scan.DefaultDepth, render.SortCycle)
// so the help text can never drift from the code defaults — no hand-typed "4"
// or sort list here.
var usage = fmt.Sprintf(`prv — projects directory viewer (read-only)

Usage:
  prv [tui] [<path>] [flags]        live TUI (falls back to table when not a TTY)
  prv ls [<path>] [flags]           print a fixed-width table to stdout

Flags:
  --depth=<n>            root-relative classification + manifest/TODO search depth (default %d)
  --fetch                fetch tracking refs before rendering (network)
  --refresh=<duration>   TUI auto-rescan cadence (e.g. --refresh=30s)
  --sort=<field>         sort field (ls): %s
`, scan.DefaultDepth, strings.Join(render.SortCycle(), " | "))

type options struct {
	fetch   bool
	refresh time.Duration
	sort    string
	cfg     scan.Config
	// zero means no flag supplied; refresh disabled by default per spec
	refreshSet bool
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer, stderr io.Writer) error {
	mode, rest, err := splitSubcommand(args)
	if err != nil {
		return err
	}

	var (
		fs       = flag.NewFlagSet("prv", flag.ContinueOnError)
		opts     options
		sortFlag string
	)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usage) }
	fs.BoolVar(&opts.fetch, "fetch", false, "fetch tracking refs (network)")
	fs.DurationVar(&opts.refresh, "refresh", 0, "TUI auto-rescan cadence")
	fs.StringVar(&sortFlag, "sort", render.DefaultSort(), "sort field ("+strings.Join(render.SortCycle(), "|")+")")
	var depth int
	fs.IntVar(&depth, "depth", scan.DefaultDepth, "root-relative classification + manifest/TODO search depth")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if fs.Lookup("refresh").Value.String() != "0s" {
		opts.refreshSet = true
	}
	if !render.ValidSort(sortFlag) {
		return fmt.Errorf("invalid --sort %q: want one of %s", sortFlag, strings.Join(render.SortCycle(), ", "))
	}
	opts.sort = sortFlag
	opts.cfg = scan.Config{Depth: depth}

	// At most one positional path remains; default to ".".
	path := "."
	if fs.NArg() > 1 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if fs.NArg() == 1 {
		path = fs.Arg(0)
	}

	switch mode {
	case "ls":
		return runLS(path, opts, stdout)
	case "tui":
		if !isTTY(stdout) {
			// Locked fallback: no TTY → table output.
			return runLS(path, opts, stdout)
		}
		return runTUI(path, opts, stdout)
	}
	return nil
}

// splitSubcommand returns the frontend ("ls" or "tui") and the remaining args.
// Anything that isn't an explicit "ls"/"tui" token is treated as tui mode
// with that token as the first positional path.
func splitSubcommand(args []string) (string, []string, error) {
	if len(args) == 0 {
		return "tui", nil, nil
	}
	switch args[0] {
	case "ls", "tui":
		return args[0], args[1:], nil
	}
	if strings.HasPrefix(args[0], "-") {
		// Flags first; subcommand is implicit tui.
		return "tui", args, nil
	}
	// First non-flag token = path; implicit tui.
	return "tui", args, nil
}

func isTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// runLS prints a fixed-width table for the scan root. Stateless: one scan,
// one sort, one render, no cache. --fetch fetches tracking refs (network)
// first, then re-derives ahead/behind from the fresh refs. fresh is true only
// when --fetch actually ran and succeeded, so a failed fetch still hedges
// local-only ahead/behind with the `?` stale suffix (same contract as the
// TUI): false → render.UpDown appends `?`; true → plain arrows.
func runLS(path string, opts options, stdout io.Writer) error {
	rows := scan.Run(path, opts.cfg)
	fresh := false
	if opts.fetch {
		// Graceful: network failures keep local-only values (and fresh stays
		// false so the rendered ahead/behind hedges them, not misreads them).
		fresh = scan.FetchRows(rows, opts.cfg) == nil
	}
	render.Sort(rows, opts.sort)
	fmt.Fprint(stdout, report.RenderTable(rows, fresh))
	return nil
}

// runTUI runs the full-screen Bubble Tea model. Statelessness holds: the model
// re-scans from disk on init, `r`, and after fetch. fetchHook wires the TUI's
// `f` key to the hardened scan.FetchRows (network errors degrade gracefully).
func runTUI(path string, opts options, stdout io.Writer) error {
	cfg := opts.cfg
	model := tui.New(path, cfg, opts.refresh, func(rows []project.Project) error {
		return scan.FetchRows(rows, cfg)
	})
	prog := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		return err
	}
	return nil
}

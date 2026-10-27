// Command srchdsh is a metasearch backend for DSH.
//
// It is a fork of srchd (https://git.sr.ht/~cmcevoy/srchd) with the web
// interface removed. `srchdsh search` emits one JSON document on stdout for a
// program to consume; `srchdsh serve` exposes the same document over HTTP for
// a second consumer.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"srchdsh/search"

	_ "srchdsh/search/engines"
)

// Exit codes, part of the contract with the caller.
const (
	exitOK        = 0
	exitNoResults = 1
	exitFailure   = 2
)

var blacklist = newBlacklist()

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run dispatches a command and returns the process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	log.SetOutput(stderr)

	if len(args) == 0 {
		usage(stderr)
		return exitFailure
	}

	switch args[0] {
	case "search":
		return runSearch(args[1:], stdout, stderr)
	case "serve":
		return runServe(args[1:], stdout, stderr)
	case "engines":
		return runEngines(args[1:], stdout, stderr)
	case "version", "-version", "--version", "-V":
		fmt.Fprintf(stdout, "srchdsh %s\n", Version)
		return exitOK
	case "help", "-h", "-help", "--help":
		usage(stdout)
		return exitOK
	default:
		fmt.Fprintf(stderr, "srchdsh: unknown command %q\n\n", args[0])
		usage(stderr)
		return exitFailure
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `srchdsh — metasearch backend for DSH

usage:
  srchdsh search -q QUERY [-max N] [-page N] [-json|-table] [-conf FILE]
  srchdsh serve [-addr 127.0.0.1:8089] [-conf FILE]
  srchdsh engines [-conf FILE]
  srchdsh version

search writes one JSON document to stdout when stdout is not a terminal, and a
table when it is; -json and -table override that choice. Diagnostics always go
to stderr, so a pipe sees clean JSON.

exit codes:
  0  at least one source
  1  no sources, but no hard failure
  2  failure (bad usage, no engines, or every engine failed)

query operators:
  :engine restricts the search to the named engines, e.g.
  "srchdsh search -q ':ddg deepseek harness'"; use \: for a literal colon.

configuration:
  YAML, read from $XDG_CONFIG_HOME/srchdsh/config.yaml when present, or from
  the path given with -conf. Without either, defaults apply.
`)
}

// runSearch implements the `search` command.
func runSearch(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		query   string
		max     int
		page    int
		conf    string
		jsonOut bool
		table   bool
		timeout time.Duration
	)

	fs.Usage = func() {
		fmt.Fprint(stderr, "usage: srchdsh search -q QUERY [-max N] [-page N] [-json|-table] [-conf FILE]\n\n")
		fmt.Fprint(stderr, "Searches every enabled engine and merges the results.\n"+
			"Diagnostics go to stderr; stdout carries only the document or table.\n\n"+
			"query operators:\n"+
			"  :engine  restrict to the named engines, e.g. ':ddg deepseek harness'\n"+
			"  \\:       search for a literal colon\n\n")
		fs.PrintDefaults()
	}

	fs.StringVar(&query, "q", "", "query to search for")
	fs.StringVar(&query, "query", "", "query to search for")
	fs.IntVar(&max, "max", 10, "maximum number of sources; 0 means no limit")
	fs.IntVar(&page, "page", 1, "result page, starting at 1")
	fs.StringVar(&conf, "conf", "", "configuration file")
	fs.BoolVar(&jsonOut, "json", false, "emit the JSON document")
	fs.BoolVar(&table, "table", false, "emit a human-readable table")
	fs.DurationVar(&timeout, "timeout", 30*time.Second, "overall search timeout")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitFailure
	}

	// A bare query is accepted too: `srchdsh search deepseek harness`.
	if query == "" {
		query = strings.Join(fs.Args(), " ")
	}
	if strings.TrimSpace(query) == "" {
		fmt.Fprintln(stderr, "srchdsh search: a query is required (-q or as an argument)")
		return exitFailure
	}
	if page < 1 {
		fmt.Fprintln(stderr, "srchdsh search: -page starts at 1")
		return exitFailure
	}
	if max < 0 {
		fmt.Fprintln(stderr, "srchdsh search: -max cannot be negative")
		return exitFailure
	}

	if err := initSearch(conf); err != nil {
		fmt.Fprintf(stderr, "srchdsh: %v\n", err)
		return exitFailure
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	started := time.Now()
	out := doSearch(ctx, query, page-1)
	took := time.Since(started)

	if out.Err != nil && !errors.Is(out.Err, errAllFailed) {
		fmt.Fprintf(stderr, "srchdsh: %v\n", out.Err)
		return exitFailure
	}

	doc := buildDocument(out.Query, out.Results, out.Runs, max, took)
	asJSON := jsonOut || (!table && !isTerminal(stdout))

	if asJSON {
		if err := writeJSON(stdout, doc); err != nil {
			fmt.Fprintf(stderr, "srchdsh: write results: %v\n", err)
			return exitFailure
		}
	} else {
		writeTable(stdout, doc)
		writeEngineErrors(stderr, out.Runs)
	}

	switch {
	case errors.Is(out.Err, errAllFailed):
		return exitFailure
	case len(doc.Sources) == 0:
		return exitNoResults
	default:
		return exitOK
	}
}

// runEngines lists the enabled engines.
func runEngines(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("engines", flag.ContinueOnError)
	fs.SetOutput(stderr)

	conf := fs.String("conf", "", "configuration file")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitFailure
	}

	if err := initSearch(*conf); err != nil {
		fmt.Fprintf(stderr, "srchdsh: %v\n", err)
		return exitFailure
	}

	defaults := search.DefaultEngines()
	names := make([]string, 0, len(engines))
	for name := range engines {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if contains(defaults, name) {
			fmt.Fprintf(stdout, "%s (default)\n", name)
			continue
		}
		fmt.Fprintf(stdout, "%s (configured)\n", name)
	}

	return exitOK
}

// initSearch loads the configuration, blacklists and engine instances.
func initSearch(conf string) error {
	path, err := resolveConfigPath(conf)
	if err != nil {
		return err
	}

	if path != "" {
		if err := loadConfig(path); err != nil {
			return fmt.Errorf("load config %s: %w", path, err)
		}
	}

	for _, v := range cfg.Blacklists {
		n, err := blacklist.LoadFile(v)
		if err != nil {
			// A missing blocklist should not stop a search.
			log.Printf("failed to load blacklist %q: %v", v, err)
			continue
		}
		log.Printf("loaded %d rules from %s", n, v)
	}

	for _, name := range enabledEngines() {
		eng, err := initializeEngine(name)
		if err != nil {
			return fmt.Errorf("initialize engine %q: %w", name, err)
		}
		engines[name] = eng
	}

	return nil
}

// resolveConfigPath determines which configuration file to read.
//
// An explicit path must exist. Without one, the user configuration file is used
// when it exists, and no configuration otherwise.
func resolveConfigPath(explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", fmt.Errorf("config %s: %w", explicit, err)
		}
		return explicit, nil
	}

	dir, err := os.UserConfigDir()
	if err != nil {
		return "", nil
	}

	path := filepath.Join(dir, "srchdsh", "config.yaml")
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("config %s: %w", path, err)
	}

	return path, nil
}

// isTerminal reports whether w is a terminal, used to pick the output format.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}

	info, err := f.Stat()
	if err != nil {
		return false
	}

	return info.Mode()&os.ModeCharDevice != 0
}

// contains is a small helper for the engine listings.
func contains(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

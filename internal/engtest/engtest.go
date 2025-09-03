// Package engtest includes a number of helpers for testing search engines.
//
// engtest aids in testing engines and comparing any number of inputs with
// their expected outputs.
// It does this by mocking the HTTP client and then attempting to search as
// normal.
//
// # Considerations for queries
//
// Some search engines don't have stable output and as a result your engine
// might need to work with several different types of responses; Google is such
// an engine where sometimes a result is slightly different from the rest and a
// complete extraction of all data for a result might not happen.
// While the Google engine is usually capable of finding the title and link, it
// can have trouble with the description.
//
// As such, try to construct enough test cases to maximize your coverage.
package engtest

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"git.sr.ht/~cmcevoy/srchd/search"
)

// Tester is the contextual struct for testing engines.
type Tester struct {
	// driver is the name of the engine.
	driver string

	// cfg is the base configuration to use for engine initialization.
	cfg search.Config

	Config
}

// Special configuration options for engtest.
type Config struct {
	// When set, engtest will ignore empty descriptions in search results.
	IgnoreEmptyDescription bool
}

var (
	// Specifying the -update option when testing (go test -update ./...)
	// will actually perform the requests and save the updated responses
	// instead of simply mocking them and reading them from disk.
	update = flag.Bool("update", false, "Update test fixtures by actually doing the request")

	updateWaitTimeSecs = flag.Int("updatewait", 3, "Amount of time to wait between updates")
)

// New creates a new Tester from an engine name and a base config.
//
// The contents of the configuration struct is not important, however engtest
// will always set a name based on the test case that is being run and will
// flip Debug to true.
//
// If a driver is not supported, New panics.
func New(driver string, cfg search.Config, ecfg ...Config) *Tester {
	// Make sure that the engine is even supported.
	if !slices.Contains(search.Supported(), driver) {
		panic(fmt.Sprintf("search engine %q not supported", driver))
	}

	cfg.Type = driver
	cfg.Debug = true

	// Use engtest config if provided
	ec := Config{}
	if len(ecfg) > 0 {
		ec = ecfg[0]
	}

	return &Tester{
		driver: driver,
		cfg:    cfg,
		Config: ec,
	}
}

// RunTest runs a test for an engine on a specific query.
//
// engtest will initialize the engine, load the test file, and then will
// perform a query while mocking the other end of the connection.
//
// If -update was passed to go test, then the search queries will actually be
// performed and their results will be saved to disk.
//
// The return value indicates if the tests were updated or not.
// Tests are updated when -update is passed to go test, or when test files do
// not exist for this test.
func (tt *Tester) RunTest(t *testing.T, query string) (updated bool) {
	// Test to see if this exists.
	fp := filepath.Join("testdata", tt.driver, fnencode(query))
	notExist := false
	if _, err := os.Stat(fp); err != nil {
		// I *could* check the error, yes...
		notExist = true
	}

	// Determine what mode to enter
	testFn := tt.mockTestFn(query)
	if *update || notExist {
		if !*update {
			t.Logf("automatically entering update mode for %q", query)
		}
		testFn = tt.updateTestFn(query)
		updated = true
	}

	t.Run(fmt.Sprintf("%s:%q", tt.driver, query), testFn)
	return
}

// RunTests is a wrapper function around [RunTest] to run several tests at
// once.
//
// If -update is passed to go test, RunTests will introduce a 3 second delay
// between test runs to prevent your IP address from being blocked from
// accessing the search engine.
func (tt *Tester) RunTests(t *testing.T, queries ...string) {
	for i, q := range queries {
		updated := tt.RunTest(t, q)

		if updated && i != len(queries)-1 {
			time.Sleep(time.Second * time.Duration(*updateWaitTimeSecs))
		}
	}
}

// Mocks the remote end and compares results.
func (t *Tester) mockTestFn(query string) func(t *testing.T) {
	fp := filepath.Join("testdata", t.driver, fnencode(query))
	tp := &mockTransport{
		Update: false,
		Base:   fp,
	}

	return func(tt *testing.T) {
		// Create a new HTTP client and setup the transport.
		client := t.cfg.NewHttpClient()
		client.Client().Transport = tp

		// Initialize the engine keeping in mind the new fresh HttpClient.
		cfg := t.cfg
		cfg.HttpClient = client
		eng, err := cfg.New()
		if err != nil {
			tt.Fatalf("unable to initialize engine: %v", err)
		}

		// Perform the query.
		res, err := eng.Search(context.TODO(), query, 0)
		if err != nil {
			tt.Fatalf("query failed: %v", err)
		}

		t.compareResults(tt, query, res)
	}
}

// Performs the search query and saves the results.
func (t *Tester) updateTestFn(query string) func(tt *testing.T) {
	fp := filepath.Join("testdata", t.driver, fnencode(query))
	tp := &mockTransport{
		Update: true,
		Base:   fp,
	}

	return func(tt *testing.T) {
		// Create a new HTTP client and setup the transport.
		client := t.cfg.NewHttpClient()
		client.Client().Transport = tp

		// Initialize the engine keeping in mind the new fresh HttpClient.
		cfg := t.cfg
		cfg.HttpClient = client
		eng, err := cfg.New()
		if err != nil {
			tt.Fatalf("unable to initialize engine: %v", err)
		}

		// Perform the query.
		res, err := eng.Search(context.TODO(), query, 0)
		if err != nil {
			tt.Fatalf("query failed: %v", err)
		} else if len(res) == 0 {
			tt.Fatalf("query returned zero results")
		}

		if t.saveResults(tt, query, res) {
			tt.Logf("updated test files for %q", query)
		} else {
			tt.Logf("updated test files %q; results are likely incorrect", query)
			tt.Logf("please refer to docs/testing.md for further instructions")
		}
	}
}

// hasEmptyField returns a non-empty string if res has a field that is empty.
//
// The returned string is the name of the field that is empty.
func hasEmptyField(res search.Result) string {
	if res.Title == "" {
		return "title"
	} else if res.Link == "" {
		return "link"
	} else if res.Description == "" {
		return "description"
	}

	return ""
}

// Save the results from the engine itself to disk.
//
// This is called only by [updateTestFn].
func (t *Tester) saveResults(tt *testing.T, query string, res []search.Result) bool {
	// Strip sources and remove score.
	ok := true
	for i := range res {
		res[i].Sources = nil
		res[i].Score = 0

		if v := hasEmptyField(res[i]); v != "" {
			if !t.IgnoreEmptyDescription || v != "description" {
				tt.Errorf("res #%d has empty field %s", i, v)
				ok = false
			}
		}
	}

	fp := filepath.Join("testdata", t.driver, fnencode(query))
	if err := os.MkdirAll(fp, 0755); err != nil {
		panic(err)
	}

	h, err := os.Create(filepath.Join(fp, "results.json"))
	if err != nil {
		panic(err)
	}
	defer h.Close()

	if err := json.NewEncoder(h).Encode(res); err != nil {
		panic(err)
	}

	return ok
}

// Compare results from the engine to expected results.
//
// This is called only by [mockTestFn].
func (t *Tester) compareResults(tt *testing.T, query string, res []search.Result) {
	// Open and load the stored results.
	fp := filepath.Join("testdata", t.driver, fnencode(query))
	h, err := os.Open(filepath.Join(fp, "results.json"))
	if err != nil {
		tt.Fatalf("failed to open results: %v", err)
	}
	defer h.Close()

	exp := []search.Result{}
	if err := json.NewDecoder(h).Decode(&exp); err != nil {
		tt.Fatalf("failed to decode results.json: %v", err)
	}

	// Check to see if they are equal.
	if len(exp) != len(res) {
		tt.Errorf("length mismatch; expected %d, got %d", len(exp), len(res))
	}

	for i := 0; i < min(len(exp), len(res)); i++ {
		v := exp[i]

		// Sources and score does not matter.
		// The JSON will already reflect this.
		res[i].Sources = nil
		res[i].Score = 0

		if !reflect.DeepEqual(v, res[i]) {
			tt.Errorf("res #%d differs: expected %v, got %v", i, v, res[i])
		}

		if v := hasEmptyField(res[i]); v != "" {
			if !t.IgnoreEmptyDescription || v != "description" {
				tt.Errorf("res #%d has empty field %s", i, v)
			}
		}
	}

	// Note that reflect.DeepEqual(res, exp) is not used because some
	// results may be affected and some results may not.
}

// Reencodes queries to be marginally more safer as filenames.
func fnencode(query string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' {
			return r
		} else if r >= 'A' && r <= 'Z' {
			return 'a' + (r - 'A')
		}
		return '-'
	}, query)
}

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"srchdsh/search"
)

const maxTitleLen = 100
const maxDescriptionLen = 300

var engines = map[string]search.Engine{}
var errAllFailed = errors.New("no engines performed a query successfully")

// engineRun is the outcome of one engine's attempt at a query.
type engineRun struct {
	// ID is the engine's name, as used in the :engine operator.
	ID string

	// Duration is how long the attempt took.
	Duration time.Duration

	// Count is how many results the engine contributed, after blacklisting.
	Count int

	// Err is why the attempt failed, if it did.
	Err error
}

// Handles the ':' search operator which specifies specific engines to search.
func processOperators(query string) (requestedEngines []string, newQuery string) {
	if !strings.ContainsRune(query, ':') {
		// No colon operator, so nothing to change.
		return nil, query
	}

	toks := strings.Split(query, " ")

	for i, tok := range toks {
		if strings.HasPrefix(tok, "\\:") {
			toks[i] = tok[1:] // Remove the backslash
		} else if strings.HasPrefix(tok, ":") {
			// Just set it to a blank string.
			// This should change nothing with a search query.
			toks[i] = ""
			requestedEngines = append(requestedEngines, tok[1:])
		}
	}

	newQuery = strings.TrimSpace(strings.Join(toks, " "))
	return
}

// Normalizes a link by passing it through [net/url].
func normalizeLink(link string) string {
	purl, err := url.Parse(link)
	if err != nil {
		return link
	}

	if purl.Path == "" {
		// At minimum it should be "/"
		purl.Path = "/"
	}

	return purl.String()
}

// Calculates the multiplier of the result score.
func calculateWeight(res search.Result) float64 {
	sum := 0.0

	for _, name := range res.Sources {
		val := 1.0

		// Override the above value if there was one set.
		engineConfig, ok := cfg.Engines[name]
		if ok {
			// Ensure the weight is 1.
			val = engineConfig.Weight
			if val == 0 {
				val = 1
			}
		}

		sum += val
	}

	return sum
}

// Calculates the score to sort against.
func calculateSortingScore(res search.Result) float64 {
	weight := calculateWeight(res)
	return weight * res.Score
}

// Truncates a string to n letters.
func truncate(s string, n int) string {
	if len(s) <= n || utf8.RuneCountInString(s) <= n {
		return s
	}

	// We must trim.
	// Go provides zero handholding when it comes to this, so this is
	// implemented using slices.
	ptr := 0
	for pos := range s {
		if ptr == n {
			return s[:pos] + "…"
		}
		ptr++
	}

	panic("unreachable")
}

// Merges and sorts results.
func processResults(res []search.Result) []search.Result {
	// Track the first time we see a link and move stuff around.
	firstSeen := map[string]int{}

	for i := 0; i < len(res); i++ {
		link := rewriteUrl(normalizeLink(res[i].Link))
		if link == "" {
			// Drop this result because it's invalid OR was
			// explicitly removed (replace: "").
			// TODO: What's a good way to move forward with this?
			// Just log it?
			res[i], res[len(res)-1] = res[len(res)-1], res[i]
			res = res[:len(res)-1]
			i-- // retry

			continue
		}

		// Update link. TODO: Move this out of here, maybe.
		res[i].Link = link

		idx, ok := firstSeen[link]
		if !ok {
			// First occurrence.
			firstSeen[link] = i
			res[i].Score = 1

			// Ensure all fields are proper before we continue.
			res[i].Title = truncate(res[i].Title, maxTitleLen)
			res[i].Description = truncate(res[i].Description, maxDescriptionLen)
			continue
		}

		// Add in the engine source(s).
		// Technically there's only supposed to be one, so this may be unnecessary.
		for _, name := range res[i].Sources {
			if !slices.Contains(res[idx].Sources, name) {
				res[idx].Sources = append(res[idx].Sources, name)
			}
		}

		// If we're missing text, replace it with this.
		if res[idx].Title == "" {
			res[idx].Title = truncate(res[i].Title, maxTitleLen)
		}
		if res[idx].Description == "" {
			res[idx].Description = truncate(res[i].Description, maxDescriptionLen)
		}

		// Swap with the last element and shrink the slice.
		res[i], res[len(res)-1] = res[len(res)-1], res[i]
		res = res[:len(res)-1]

		// Increment the score.
		// This is for sorting; results seen several times will appear
		// higher in the search results.
		res[idx].Score++

		// Decrement i so we can try the next element.
		i--
	}

	// Sort based upon the score.
	sort.Slice(res, func(i, j int) bool {
		// > is used so the results are descending and not ascending.
		return calculateSortingScore(res[i]) > calculateSortingScore(res[j])
	})

	// Return the (modified) slice.
	return res
}

// searchOutcome is everything a caller needs to build a document.
type searchOutcome struct {
	// Query is the query that was searched, with operators removed.
	Query string

	// Results are the merged and ranked results.
	Results []search.Result

	// Runs describes what every selected engine did.
	Runs []engineRun

	// Err is nil when at least one engine answered, [errAllFailed] when
	// every selected engine failed, and some other error when the request
	// itself was unusable.
	Err error
}

// doSearch queries the requested engines and merges their results.
//
// Individual engine failures do not fail the search and are reported in the
// returned runs instead; when every selected engine fails, Err is
// [errAllFailed] and the runs explain why.
func doSearch(ctx context.Context, requestQuery string, page int) searchOutcome {
	wantEngines, query := processOperators(requestQuery)
	query = strings.TrimSpace(query)

	out := searchOutcome{Query: query}
	if query == "" {
		out.Err = fmt.Errorf("empty query")
		return out
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results = []search.Result{}
		runs    = []engineRun{}
	)

	fn := func(name string, e search.Engine) {
		defer wg.Done()

		then := time.Now()
		res, err := e.Search(ctx, query, page)
		run := engineRun{
			ID:       name,
			Duration: time.Since(then),
		}

		if err != nil {
			run.Err = err
			log.Printf("searching %q failed: %v", name, err)
		} else {
			res, _ = blacklist.Filter(res)
			run.Count = len(res)
		}

		mu.Lock()
		defer mu.Unlock()

		runs = append(runs, run)
		results = append(results, res...)
	}

	selected := 0
	for name, eng := range engines {
		if len(wantEngines) > 0 && !slices.Contains(wantEngines, name) {
			continue
		}

		selected++
		wg.Add(1)
		go fn(name, eng)
	}

	if selected == 0 {
		if len(wantEngines) > 0 {
			out.Err = fmt.Errorf("no known engine selected (asked for %s)", strings.Join(wantEngines, ", "))
		} else {
			out.Err = fmt.Errorf("no engines are enabled")
		}
		return out
	}

	wg.Wait()

	// Sort the diagnostics so the JSON document is stable regardless of
	// which engine answered first.
	sort.Slice(runs, func(i, j int) bool { return runs[i].ID < runs[j].ID })

	failed := 0
	for _, run := range runs {
		if run.Err != nil {
			failed++
		}
	}

	out.Results = processResults(results)
	out.Runs = runs
	if failed == selected {
		out.Err = errAllFailed
	}

	return out
}

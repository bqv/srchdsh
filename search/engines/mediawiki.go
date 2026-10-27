package engines

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"srchdsh/search"
)

// User agent to send requests with.
type mediawiki struct {
	name     string
	endpoint string
	http     *search.HttpClient
}

var (
	_ search.Engine = &mediawiki{}
)

func init() {
	// Default is false because this requires configuration
	search.Add("mediawiki", false, func(config search.Config) (search.Engine, error) {
		var ep string
		var ok bool

		if config.Extra == nil {
			return nil, errors.New("no extra configuration despite being required")
		}

		if _, ok = config.Extra["endpoint"]; !ok {
			return nil, errors.New("endpoint not specified")
		}

		if ep, ok = config.Extra["endpoint"].(string); !ok {
			return nil, errors.New("endpoint is not a string")
		}

		// TODO: Is endpoint valid?

		return &mediawiki{
			name:     config.Name,
			endpoint: ep,
			http:     config.NewHttpClient(),
		}, nil
	})
}

func (w *mediawiki) Search(ctx context.Context, query string, page int) ([]search.Result, error) {
	form := url.Values{}

	if page > 1 {
		// MediaWiki doesn't support offset
		return nil, nil
	}

	form.Set("action", "opensearch")
	form.Set("search", query)
	form.Set("limit", "10") // Arbitrary
	form.Set("profile", "fuzzy")
	form.Set("format", "json")
	form.Set("namespace", "0")

	ctx, cancel := w.http.Context(ctx)
	defer cancel()

	res, err := w.http.Get(ctx, w.endpoint+"?"+form.Encode())
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var wres []any
	if err := json.NewDecoder(res.Body).Decode(&wres); err != nil {
		return nil, err
	}

	if len(wres) != 4 {
		return nil, fmt.Errorf("expected %d arrays, got %d", 4, len(wres))
	}

	// Ensure everything is as we expect
	var titles, descriptions, links []any
	var ok bool
	if titles, ok = wres[1].([]any); !ok {
		return nil, fmt.Errorf("expected []any in second field, got %T", wres[1])
	} else if descriptions, ok = wres[2].([]any); !ok {
		return nil, fmt.Errorf("expected []any in third field, got %T", wres[2])
	} else if links, ok = wres[3].([]any); !ok {
		return nil, fmt.Errorf("expected []any in fourth field, got %T", wres[3])
	}

	results := make([]search.Result, len(wres[1].([]any)))
	for i := range results {
		// Sanity checking

		title, ok := titles[i].(string)
		if !ok {
			return nil, fmt.Errorf("result %d has invalid title type %T", i, titles[i])
		}

		desc, ok := descriptions[i].(string)
		if !ok {
			return nil, fmt.Errorf("result %d has invalid description type %T", i, descriptions[i])
		}

		link, ok := links[i].(string)
		if !ok {
			return nil, fmt.Errorf("result %d has invalid link type %T", i, links[i])
		}

		// All good!
		results[i] = search.Result{
			Title:       title,
			Description: desc,
			Link:        link,
			Sources:     []string{w.name},
		}
	}

	return results, nil
}

// Ping checks to see if the engine is reachable.
func (w *mediawiki) Ping(ctx context.Context) error {
	// Just access the index to see if we're okay.
	_, err := w.http.Get(ctx, w.endpoint)
	return err
}

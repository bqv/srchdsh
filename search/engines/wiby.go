package engines

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"

	"srchdsh/search"
)

// User agent to send requests with.
type wiby struct {
	name string
	http *search.HttpClient
}

type wibyResult struct {
	URL     string
	Title   string
	Snippet string
}

var (
	_ search.Engine = &wiby{}
)

func init() {
	search.Add("wiby", true, func(config search.Config) (search.Engine, error) {
		return &wiby{
			name: config.Name,
			http: config.NewHttpClient(),
		}, nil
	})
}

func (w *wiby) toNativeResult(r wibyResult) search.Result {
	// wiby escapes all text for direct inclusion in HTML, presumably.
	// Go's text/template does this for us, so text should be unescaped
	// here to prevent "&amp;" and other similar escapes from appearing as
	// text in the results page.
	return search.Result{
		Link:        html.UnescapeString(r.URL),
		Title:       html.UnescapeString(r.Title),
		Description: html.UnescapeString(r.Snippet),
		Sources:     []string{w.name},
	}
}

func (w *wiby) Search(ctx context.Context, query string, page int) ([]search.Result, error) {
	// Wiby has a native API we can use.
	// There's probably some encoding/json tomfoolery I could employ so we
	// don't need an intermediate step, but whatever.

	form := url.Values{}

	form.Set("q", query)
	if page > 1 {
		form.Set("p", fmt.Sprint(page+1))
	}

	ctx, cancel := w.http.Context(ctx)
	defer cancel()

	res, err := w.http.Get(ctx, "https://wiby.me/json?"+form.Encode())
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var wres []wibyResult
	if err := json.NewDecoder(res.Body).Decode(&wres); err != nil {
		return nil, err
	}

	results := make([]search.Result, len(wres))
	for i := range results {
		results[i] = w.toNativeResult(wres[i])
	}

	return results, nil
}

// Ping checks to see if the engine is reachable.
func (w *wiby) Ping(ctx context.Context) error {
	// Just access the index to see if we're okay.
	_, err := w.http.Get(ctx, "https://wiby.me/")
	return err
}

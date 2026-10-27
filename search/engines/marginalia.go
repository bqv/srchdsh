package engines

// NOTE: I am somewhat conflicted on adding this in since this is a FOSS
// project that I do enjoy using (it somewhat inspired my own search engine,
// indx), especially as I am parsing the HTML instead of going through the API.
// I am going to add it nonetheless since it is likely going to be a net
// positive for me since I often go to Marginalia if I can't find something
// through srchd or if I'm looking for pages from specific types of websites.

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"srchdsh/search"
)

type marginalia struct {
	name string
	http *search.HttpClient
}

var (
	_ search.Engine = &marginalia{}
)

func init() {
	search.Add("marginalia", true, func(config search.Config) (search.Engine, error) {
		return &marginalia{
			name: config.Name,
			http: config.NewHttpClient(),
		}, nil
	})
}

// Search attempts to query the engine and returns a number of results.
func (d *marginalia) Search(ctx context.Context, query string, page int) ([]search.Result, error) {
	q := url.Values{}
	q.Set("query", query)
	if page >= 1 {
		q.Set("page", fmt.Sprint(page+1))
	}

	ctx, cancel := d.http.Context(ctx)
	defer cancel()

	_, doc, err := d.http.HtmlGet(
		ctx,
		"https://marginalia-search.com/search?"+q.Encode(),
	)
	if err != nil {
		return nil, err
	}

	// Marginalia's HTML probably intentionally does not use class names
	// that matter to us
	elem := doc.Find(`body > div > div > main > div`).Eq(0).Children()

	results := make([]search.Result, int(elem.Length()))

	for i := range results {
		v := search.Result{}

		e := elem.Eq(i)
		title := e.Find("h2 > a")
		v.Link, _ = title.Attr("href")
		v.Link = search.CleanURL(v.Link)
		v.Title = title.Text()
		v.Description = strings.TrimSpace(e.Find("p").Text())

		v.Sources = []string{d.name}

		results[i] = v
	}

	return results, nil
}

// Ping checks to see if the engine is reachable.
func (d *marginalia) Ping(ctx context.Context) error {
	// Just access the index to see if we're okay.
	_, err := d.http.Get(ctx, "https://marginalia-search.com")
	return err
}

package engines

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"srchdsh/search"
)

// User agent to send requests with.
type yahoo struct {
	name string
	http *search.HttpClient
}

var (
	_ search.Engine = &yahoo{}
)

func init() {
	search.Add("yahoo", true, func(config search.Config) (search.Engine, error) {
		return &yahoo{
			name: config.Name,
			http: config.NewHttpClient(),
		}, nil
	})
}

func decodeYahooHref(href string) string {
	// In the form of:
	// https://r.search.yahoo.com/.../.../RU=yourlinkishere/.../...

	idx := strings.Index(href, "/RU=")
	if idx == -1 {
		return href
	}

	newHref := href[idx+4:]

	idx = strings.Index(newHref, "/")
	if idx == -1 {
		return href
	}

	newHref = newHref[:idx]
	newHref, err := url.PathUnescape(newHref)
	if err != nil {
		return href
	}
	return newHref
}

// Search attempts to query the engine and returns a number of results.
func (b *yahoo) Search(ctx context.Context, query string, page int) ([]search.Result, error) {
	form := url.Values{}

	form.Set("p", query)
	form.Set("nojs", "1")

	if page >= 1 {
		form.Set("b", fmt.Sprint(1+7*page))
		form.Set("pz", "7")
	}

	ctx, cancel := b.http.Context(ctx)
	defer cancel()

	_, doc, err := b.http.HtmlGet(
		ctx,
		"https://search.yahoo.com/search?"+form.Encode(),
	)
	if err != nil {
		return nil, err
	}

	elem := doc.Find(`.algo`)

	results := make([]search.Result, elem.Length())

	for i := range results {
		v := search.Result{}

		e := elem.Eq(i)

		// There are now two styles of results, so we need to handle
		// each of them differently.
		// I think this is the best way to do it?
		if title := e.Find("h3.title > a"); title.Length() != 0 {
			// Old style
			title.Find(`span`).Remove()
			v.Link, _ = title.Attr("href")
			v.Link = search.CleanURL(decodeYahooHref(v.Link))
			v.Title = title.Text()
			v.Description = strings.TrimSpace(e.Find(".compText > p").Text())
		} else {
			// New style
			v.Link, _ = e.Find(`a[data-matarget="algo"]`).Attr("href")
			v.Link = search.CleanURL(decodeYahooHref(v.Link))
			v.Title = e.Find("h3.title").Text()
			v.Description = strings.TrimSpace(e.Find(".compText > p").Text())
		}

		v.Sources = []string{b.name}

		results[i] = v
	}

	return results, nil
}

// Ping checks to see if the engine is reachable.
func (b *yahoo) Ping(ctx context.Context) error {
	// Just access the index to see if we're okay.
	_, err := b.http.Get(ctx, "https://search.yahoo.com/")
	return err
}

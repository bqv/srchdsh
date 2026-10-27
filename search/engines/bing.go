package engines

import (
	"context"
	"fmt"
	"net/url"

	"srchdsh/search"
)

// User agent to send requests with.
type bing struct {
	name string
	http *search.HttpClient
}

var (
	_ search.Engine = &bing{}
)

func init() {
	search.Add("bing", false, func(config search.Config) (search.Engine, error) {
		return &bing{
			name: config.Name,
			http: config.NewHttpClient(),
		}, nil
	})
}

// Search attempts to query the engine and returns a number of results.
func (b *bing) Search(ctx context.Context, query string, page int) ([]search.Result, error) {
	form := url.Values{}

	form.Set("q", query)

	if page >= 1 {
		form.Set("first", fmt.Sprint(10*page))
	}

	ctx, cancel := b.http.Context(ctx)
	defer cancel()

	_, doc, err := b.http.HtmlGet(
		ctx,
		"https://www.bing.com/search?"+form.Encode(),
	)
	if err != nil {
		return nil, err
	}

	// Remove all of these. Prepends "Web" to every result.
	doc.Find(`.algoSlug_icon`).Remove()

	elem := doc.Find(`.b_algo`)

	// Bing results are laid out like this:
	// h2 > a: title and link
	// div.b_caption > p: desc

	results := make([]search.Result, elem.Length())

	for i := range results {
		v := search.Result{}

		e := elem.Eq(i)
		title := e.Find("h2 > a")
		v.Link, _ = title.Attr("href")
		v.Link = search.CleanURL(v.Link)
		v.Title = title.Text()
		v.Description = e.Find("div > p").Text()
		v.Sources = []string{b.name}

		results[i] = v
	}

	return results, nil
}

// Ping checks to see if the engine is reachable.
func (b *bing) Ping(ctx context.Context) error {
	// Just access the index to see if we're okay.
	_, err := b.http.Get(ctx, "https://www.bing.com/")
	return err
}

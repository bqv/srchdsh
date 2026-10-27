package engines

import (
	"context"
	"fmt"
	"net/url"

	"srchdsh/search"
)

type brave struct {
	name string
	http *search.HttpClient
}

var (
	_ search.Engine = &brave{}
)

func init() {
	search.Add("brave", false, func(config search.Config) (search.Engine, error) {
		// Brave returns a lot of content in its headers, so we need to
		// increase the size that FastHTTP is willing to read
		cli := config.NewHttpClient()
		// cli.Client().ReadBufferSize = 8192

		return &brave{
			name: config.Name,
			http: cli,
		}, nil
	})
}

// Search attempts to query the engine and returns a number of results.
func (b *brave) Search(ctx context.Context, query string, page int) ([]search.Result, error) {
	form := url.Values{}

	form.Set("q", query)

	if page >= 1 {
		form.Set("offset", fmt.Sprint(page))
	}

	ctx, cancel := b.http.Context(ctx)
	defer cancel()

	_, doc, err := b.http.HtmlGet(
		ctx,
		"https://search.brave.com/search?"+form.Encode(),
	)
	if err != nil {
		return nil, err
	}

	elem := doc.Find(`#results .snippet[data-type="web"]`)

	// Brave results are laid out like this:
	// .h: link (a element)
	// .title: you can probably guess what this is
	// .snippet-description: ditto

	results := make([]search.Result, 0, int(elem.Length()))

	for i := 0; i < cap(results); i++ {
		v := search.Result{}
		e := elem.Eq(i)

		link := e.Find("a.heading-serpresult")
		v.Link, _ = link.Attr("href")
		v.Link = search.CleanURL(v.Link)

		title := e.Find(".title")
		v.Title = title.Text()

		// Brave has three types that I know about:
		// - Regular results
		// - Product results
		// - Q&A results
		// Most results are regular results so we will try that before checking anything.
		v.Description = e.Find(".snippet-description").Text()
		if v.Description == "" {
			if el := e.Find(".product"); el.Length() != 0 {
				v.Description = el.Find(".description").Text()
			} else if el := e.Find(".inline-qa"); el.Length() != 0 {
				v.Description = el.Find(".inline-qa-answer").Text()
			}
		}

		v.Sources = []string{b.name}

		results = append(results, v)
	}

	return results, nil
}

// Ping checks to see if the engine is reachable.
func (b *brave) Ping(ctx context.Context) error {
	// Just access the index to see if we're okay.
	_, err := b.http.Get(ctx, "https://search.brave.com/")
	return err
}

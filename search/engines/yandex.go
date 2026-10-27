package engines

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/PuerkitoBio/goquery"

	"srchdsh/search"
)

type yandex struct {
	name string
	http *search.HttpClient
}

var (
	_ search.Engine = &yandex{}
)

func init() {
	search.Add("yandex", false, func(config search.Config) (search.Engine, error) {
		cli := config.NewHttpClient()

		// Pretend to be curl.
		// This may not make sense but I find that I don't get as many
		// captchas when I pretend to be curl.
		// It may also have no effect and be a huge coincidence.
		cli.UserAgent = "curl/8.7.1"
		cli.BaseHeaders = http.Header{
			"Accept": []string{"*/*"},
		}

		return &yandex{
			name: config.Name,
			http: cli,
		}, nil
	})
}

func (b *yandex) isCaptcha(doc *goquery.Document) bool {
	return doc.Find("title").Text() == "Are you not a robot?"
}

// Search attempts to query the engine and returns a number of results.
func (b *yandex) Search(ctx context.Context, query string, page int) ([]search.Result, error) {
	form := url.Values{}

	form.Set("text", query)

	// I have no idea what search box this is going to, but it's the first
	// one so one can reasonably hope that it's under ownership of Yandex.
	form.Set("searchid", "1")

	form.Set("web", "1") // search everywhere
	form.Set("lr", "87")
	form.Set("frame", "1")

	if page >= 1 {
		form.Set("p", fmt.Sprint(page-1))
	}

	ctx, cancel := b.http.Context(ctx)
	defer cancel()

	_, doc, err := b.http.HtmlGet(
		ctx,
		"https://yandex.com/sitesearch?"+form.Encode(),
	)
	if err != nil {
		return nil, err
	}

	if b.isCaptcha(doc) {
		return nil, search.ErrCaptcha
	}

	elem := doc.Find(`.b-serp-item`)

	results := make([]search.Result, 0, int(elem.Length()))
	for i := 0; i < cap(results); i++ {
		v := search.Result{}
		e := elem.Eq(i)

		link := e.Find(".b-serp-item__title-link")
		v.Link, _ = link.Attr("href")
		v.Link = search.CleanURL(v.Link)

		title := e.Find(".b-serp-item__title")
		v.Title = title.Text()

		v.Description = e.Find(".b-serp-item__text").Text()

		v.Sources = []string{b.name}

		results = append(results, v)
	}

	return results, nil
}

// Ping checks to see if the engine is reachable.
func (b *yandex) Ping(ctx context.Context) error {
	// Just access the index to see if we're okay.
	_, err := b.http.Get(ctx, "https://yandex.com/")
	return err
}

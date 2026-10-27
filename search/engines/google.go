package engines

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"srchdsh/search"
)

type google struct {
	name  string
	http  *search.HttpClient
	debug bool
}

var (
	_ search.Engine = &google{}
)

func init() {
	search.Add("google", true, func(config search.Config) (search.Engine, error) {
		// Use the Links user agent which is excempt from the
		// JavaScript requirement.
		// Other text-mode web browsers work too.
		// I can't believe I'm saying this, but thank you Google for
		// recognizing that they exist!
		cli := config.NewHttpClient()
		cli.UserAgent = "Links"

		return &google{
			name:  config.Name,
			http:  cli,
			debug: config.Debug,
		}, nil
	})
}

func decodeGoogleHref(href string) string {
	// usually in the form of /url?q=...&others
	if !strings.HasPrefix(href, "/url?") {
		return href // use as-is
	}

	href = strings.TrimPrefix(href, "/url?")

	vals, err := url.ParseQuery(href)
	if err != nil {
		// Fallback to original string
		return href
	}

	u := vals.Get("q")
	if len(u) == 0 {
		// Fallback one last time
		return href
	}

	return u
}

// Parses a general query results page.
func (g *google) parseGeneral(doc *goquery.Document, query string) ([]search.Result, error) {
	elem := doc.Find(".ezO2md")

	results := make([]search.Result, 0, int(elem.Length()))

	for i := 0; i < cap(results); i++ {
		v := search.Result{}

		e := elem.Eq(i)

		// All of these are likely to change without notice
		link := e.Find("a.fuLhoc")
		desc := e.Find("td.udTCfd .FrIlee")

		v.Title = strings.ToValidUTF8(link.Find(".CVA68e").Text(), "")
		if len(v.Title) == 0 {
			// There is a very good chance that this is not the one
			continue
		}

		v.Link, _ = link.Attr("href")
		v.Link = search.CleanURL(decodeGoogleHref(v.Link))
		v.Description = strings.TrimSpace(strings.ToValidUTF8(desc.Text(), ""))
		v.Sources = []string{g.name}

		results = append(results, v)
	}

	return results, nil
}

// Search attempts to query the engine and returns a number of results.
func (g *google) Search(ctx context.Context, query string, page int) ([]search.Result, error) {
	form := url.Values{}

	form.Set("q", query)
	form.Set("ie", "UTF-8") // defaults to ISO-8859-1

	// This specific option may seem nonsensical at first, but here is some context:
	// https://tedium.co/2024/05/17/google-web-search-make-default/
	//
	// The TL;DR is that this returns the "Web" results, sans AI stuff.
	// I run srchd on a server in the United States which has this sort of
	// stuff; I don't know if it will fix any issues with the Google
	// engine, I haven't really looked that deeply into my issues, but I
	// like just pointing the finger at AI saying that it causes me all
	// sorts of problems and calling it a day.
	form.Set("udm", "14")

	if page >= 1 {
		form.Set("start", fmt.Sprint(page*10))
	}

	_, doc, err := g.http.HtmlGet(
		ctx,
		fmt.Sprintf("https://www.google.com/search?%s", form.Encode()),
	)
	if err != nil {
		return nil, err
	}

	return g.parseGeneral(doc, query)
}

// Ping checks to see if the engine is reachable.
func (g *google) Ping(ctx context.Context) error {
	// Just access the index to see if we're okay.
	_, err := g.http.Get(ctx, "https://www.google.com/")
	return err
}

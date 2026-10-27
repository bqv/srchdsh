package engines

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"

	"srchdsh/search"
)

// bing queries Bing's RSS output rather than its HTML.
//
// Bing's search page is a JavaScript shell: a plain HTTP client gets "There
// are no results" and no result markup at all. The RSS feed is the only
// structured output it serves. It returns ten items and does not page either
// (first/count either repeat the first page or return an empty channel), so
// only the first page exists for us.
type bing struct {
	name string
	http *search.HttpClient
}

var (
	_ search.Engine = &bing{}
)

func init() {
	search.Add("bing", true, func(config search.Config) (search.Engine, error) {
		return &bing{
			name: config.Name,
			http: config.NewHttpClient(),
		}, nil
	})
}

// bingFeed is the subset of Bing's RSS document we need.
type bingFeed struct {
	Channel struct {
		Items []struct {
			Title       string `xml:"title"`
			Link        string `xml:"link"`
			Description string `xml:"description"`
			PubDate     string `xml:"pubDate"`
		} `xml:"item"`
	} `xml:"channel"`
}

// Search attempts to query the engine and returns a number of results.
func (b *bing) Search(ctx context.Context, query string, page int) ([]search.Result, error) {
	// Only the first page is served by the feed.
	if page > 0 {
		return nil, nil
	}

	form := url.Values{}
	form.Set("q", query)
	form.Set("format", "rss")

	ctx, cancel := b.http.Context(ctx)
	defer cancel()

	res, err := b.http.Get(ctx, "https://www.bing.com/search?"+form.Encode())
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("bing: read feed: %w", err)
	}

	return parseBingFeed(body, b.name)
}

// parseBingFeed extracts results from an RSS document.
func parseBingFeed(body []byte, name string) ([]search.Result, error) {
	var feed bingFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("bing: decode feed: %w", err)
	}

	results := make([]search.Result, 0, len(feed.Channel.Items))
	for _, item := range feed.Channel.Items {
		if item.Link == "" {
			continue
		}

		results = append(results, search.Result{
			Title:       htmlText(item.Title),
			Description: htmlText(item.Description),
			Link:        cleanResultURL(item.Link),
			Sources:     []string{name},
		})
	}

	return results, nil
}

// Ping checks to see if the engine is reachable.
func (b *bing) Ping(ctx context.Context) error {
	// Just access the index to see if we're okay.
	_, err := b.http.Get(ctx, "https://www.bing.com/")
	return err
}

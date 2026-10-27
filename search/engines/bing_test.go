package engines

import (
	"testing"
)

func TestParseBingFeed(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="utf-8" ?><rss version="2.0"><channel>` +
		`<title>Bing: deepseek harness</title>` +
		`<item><title>DeepSeek &amp; Co</title>` +
		`<link>https://www.deepseek.com/?utm_source=bing&amp;utm_medium=feed</link>` +
		`<description>A &lt;b&gt;harness&lt;/b&gt; for coding agents.</description>` +
		`<pubDate>Mon, 26 Oct 2026 19:12:00 GMT</pubDate></item>` +
		`<item><title>Broken item</title><link></link><description>no url</description></item>` +
		`</channel></rss>`)

	results, err := parseBingFeed(body, "bing")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("got %d results, wanted 1: %+v", len(results), results)
	}

	first := results[0]
	if first.Title != "DeepSeek & Co" {
		t.Errorf("title = %q", first.Title)
	}
	if first.Link != "https://www.deepseek.com/" {
		t.Errorf("link = %q, wanted tracking parameters removed", first.Link)
	}
	if first.Description != "A harness for coding agents." {
		t.Errorf("description = %q", first.Description)
	}
	if len(first.Sources) != 1 || first.Sources[0] != "bing" {
		t.Errorf("sources = %v", first.Sources)
	}
}

func TestParseBingFeedEmpty(t *testing.T) {
	// Bing answers a blocked request with a valid but empty channel, which is
	// indistinguishable from a genuine no-results search.
	body := []byte(`<?xml version="1.0"?><rss version="2.0"><channel>` +
		`<title>Bing: nonsense</title></channel></rss>`)

	results, err := parseBingFeed(body, "bing")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("got %d results, wanted none", len(results))
	}
}

func TestParseBingFeedInvalid(t *testing.T) {
	if _, err := parseBingFeed([]byte("not xml at all"), "bing"); err == nil {
		t.Fatal("expected an error for a non-XML body")
	}
}

func TestBingParams(t *testing.T) {
	form := bingParams("deepseek harness", "en-US")

	if got := form.Get("q"); got != "deepseek harness" {
		t.Errorf("q = %q", got)
	}
	if got := form.Get("format"); got != "rss" {
		t.Errorf("format = %q", got)
	}
	if got := form.Get("mkt"); got != "en-US" {
		t.Errorf("mkt = %q", got)
	}
	if got := form.Get("setlang"); got != "en" {
		t.Errorf("setlang = %q", got)
	}

	if got := bingParams("x", "pt-BR").Get("setlang"); got != "pt" {
		t.Errorf("setlang for pt-BR = %q", got)
	}
}

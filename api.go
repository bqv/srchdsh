package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"srchdsh/search"
)

// Document is the JSON body emitted by `srchdsh search --json` and by
// `POST /v1/search`.
//
// It is the contract DSH's web seam consumes, so the field names and shapes
// are fixed: sources are already deduplicated and ranked, and per-engine
// diagnostics ride along so a broken engine is visible in the same call.
type Document struct {
	// Query is the query that was searched, after operator parsing.
	Query string `json:"query"`

	// TookMS is how long the whole search took, in milliseconds.
	TookMS int64 `json:"tookMs"`

	// Truncated is true when sources were dropped to honour the result
	// limit.
	Truncated bool `json:"truncated"`

	// Sources are the citeable results, already capped.
	Sources []Source `json:"sources"`

	// Engines reports what every selected engine did, including failures.
	Engines []EngineStatus `json:"engines"`
}

// Source is one citeable search result.
type Source struct {
	URL         string `json:"url"`
	Title       string `json:"title,omitempty"`
	Snippet     string `json:"snippet,omitempty"`
	PublishedAt string `json:"publishedAt,omitempty"`
}

// EngineStatus is the outcome of one engine for one search.
type EngineStatus struct {
	// ID is the engine's name.
	ID string `json:"id"`

	// MS is how long the engine took, in milliseconds.
	MS int64 `json:"ms"`

	// Count is how many results the engine contributed.
	Count int `json:"count"`

	// Error is a short machine-readable failure reason, such as "captcha",
	// "timeout" or "http 429". It is empty on success.
	Error string `json:"error,omitempty"`
}

// buildDocument converts search results and engine diagnostics into the
// document that both interfaces emit.
//
// max is the result limit; zero or less means no limit. Truncation is recorded
// rather than silently applied.
func buildDocument(query string, results []search.Result, runs []engineRun, max int, took time.Duration) Document {
	doc := Document{
		Query:   query,
		TookMS:  took.Milliseconds(),
		Sources: make([]Source, 0, len(results)),
		Engines: make([]EngineStatus, 0, len(runs)),
	}

	for _, res := range results {
		if max > 0 && len(doc.Sources) >= max {
			doc.Truncated = true
			break
		}

		doc.Sources = append(doc.Sources, Source{
			URL:     res.Link,
			Title:   res.Title,
			Snippet: res.Description,
		})
	}

	for _, run := range runs {
		status := EngineStatus{
			ID:    run.ID,
			MS:    run.Duration.Milliseconds(),
			Count: run.Count,
		}
		if run.Err != nil {
			status.Error = shortError(run.Err)
		}
		doc.Engines = append(doc.Engines, status)
	}

	return doc
}

// shortError reduces an engine error to the short machine strings the
// document's contract calls for.
func shortError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, search.ErrCaptcha):
		return "captcha"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	}

	var httpErr search.HttpError
	if errors.As(err, &httpErr) {
		return fmt.Sprintf("http %d", httpErr.Status)
	}

	return "error"
}

// writeJSON writes the document as one JSON object.
func writeJSON(w io.Writer, doc Document) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(doc)
}

// writeTable writes the document for a human reader.
func writeTable(w io.Writer, doc Document) {
	if len(doc.Sources) == 0 {
		fmt.Fprintln(w, "no results")
		return
	}

	for i, src := range doc.Sources {
		title := src.Title
		if title == "" {
			title = src.URL
		}

		fmt.Fprintf(w, "%2d. %s\n", i+1, title)
		fmt.Fprintf(w, "    %s\n", src.URL)
		if src.Snippet != "" {
			fmt.Fprintf(w, "    %s\n", src.Snippet)
		}
		fmt.Fprintln(w)
	}

	if doc.Truncated {
		fmt.Fprintf(w, "(%d sources shown, more available)\n", len(doc.Sources))
	}
}

// writeEngineErrors reports failed engines for a human reader. Diagnostics go
// to stderr so a machine reading stdout sees only the document.
func writeEngineErrors(w io.Writer, runs []engineRun) {
	for _, run := range runs {
		if run.Err == nil {
			continue
		}
		fmt.Fprintf(w, "%s: %v\n", run.ID, run.Err)
	}
}

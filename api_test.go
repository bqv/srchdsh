package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"srchdsh/search"
)

func TestBuildDocument(t *testing.T) {
	results := []search.Result{
		{Title: "First", Description: "One", Link: "https://example.org/1", Sources: []string{"ddg"}},
		{Title: "Second", Description: "Two", Link: "https://example.org/2", Sources: []string{"bing"}},
	}
	runs := []engineRun{
		{ID: "bing", Duration: 120 * time.Millisecond, Count: 1},
		{ID: "startpage", Duration: 240 * time.Millisecond, Err: fmt.Errorf("wrapped: %w", search.ErrCaptcha)},
	}

	doc := buildDocument("deepseek harness", results, runs, 1, 500*time.Millisecond)

	var buf bytes.Buffer
	if err := writeJSON(&buf, doc); err != nil {
		t.Fatalf("write: %v", err)
	}

	want := `{"query":"deepseek harness","tookMs":500,"truncated":true,` +
		`"sources":[{"url":"https://example.org/1","title":"First","snippet":"One"}],` +
		`"engines":[{"id":"bing","ms":120,"count":1},` +
		`{"id":"startpage","ms":240,"count":0,"error":"captcha"}]}` + "\n"

	if got := buf.String(); got != want {
		t.Fatalf("document mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestBuildDocumentEmpty(t *testing.T) {
	doc := buildDocument("nothing", nil, nil, 10, 0)

	var buf bytes.Buffer
	if err := writeJSON(&buf, doc); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Empty collections must be [], not null: the consumer iterates them.
	want := `{"query":"nothing","tookMs":0,"truncated":false,"sources":[],"engines":[]}` + "\n"
	if got := buf.String(); got != want {
		t.Fatalf("document mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestBuildDocumentNoLimit(t *testing.T) {
	results := []search.Result{
		{Link: "https://example.org/1"},
		{Link: "https://example.org/2"},
	}

	doc := buildDocument("q", results, nil, 0, 0)
	if len(doc.Sources) != 2 {
		t.Fatalf("got %d sources, wanted 2", len(doc.Sources))
	}
	if doc.Truncated {
		t.Fatal("reported truncation without a limit")
	}
}

func TestShortError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"captcha", fmt.Errorf("engine: %w", search.ErrCaptcha), "captcha"},
		{"timeout", context.DeadlineExceeded, "timeout"},
		{"cancelled", context.Canceled, "cancelled"},
		{"http", search.HttpError{Status: 429, URL: "https://x/", Method: "GET"}, "http 429"},
		{"other", errors.New("something odd"), "error"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shortError(test.err); got != test.want {
				t.Fatalf("shortError(%v) = %q, wanted %q", test.err, got, test.want)
			}
		})
	}
}

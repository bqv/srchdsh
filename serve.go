package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// searchRequest is the body of POST /v1/search.
type searchRequest struct {
	Query      string `json:"query"`
	MaxResults int    `json:"maxResults"`
	Page       int    `json:"page"`
}

// runServe implements the `serve` command: the same document as `search`, over
// HTTP, so a second consumer or a warm browser session can be added later.
func runServe(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)

	addr := fs.String("addr", "127.0.0.1:8089", "address to listen on")
	conf := fs.String("conf", "", "configuration file")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitFailure
	}

	if err := initSearch(*conf); err != nil {
		fmt.Fprintf(stderr, "srchdsh: %v\n", err)
		return exitFailure
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := &http.Server{
		Addr:    *addr,
		Handler: newMux(),

		ReadTimeout:  10 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	fmt.Fprintf(stderr, "srchdsh %s listening on http://%s\n", Version, *addr)

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(stderr, "srchdsh: %v\n", err)
			return exitFailure
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(stderr, "srchdsh: shutdown: %v\n", err)
			return exitFailure
		}
	}

	return exitOK
}

// newMux builds the serve-mode routes. There is no web interface: the only
// contract is the search document.
func newMux() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /v1/search", handleSearch)
	mux.HandleFunc("GET /health", handleHealth)

	return mux
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"ok":true,"version":%q}`+"\n", Version)
}

func handleSearch(w http.ResponseWriter, r *http.Request) {
	req := searchRequest{
		MaxResults: 10,
		Page:       1,
	}

	// A JSON body is the primary form; query parameters are accepted as a
	// convenience for curl and for a browser.
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	params := r.URL.Query()
	if req.Query == "" {
		req.Query = params.Get("q")
	}
	if v := params.Get("max"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeJSONError(w, http.StatusBadRequest, "max must be a non-negative integer")
			return
		}
		req.MaxResults = n
	}
	if v := params.Get("page"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeJSONError(w, http.StatusBadRequest, "page starts at 1")
			return
		}
		req.Page = n
	}
	if req.Page < 1 {
		req.Page = 1
	}

	if strings.TrimSpace(req.Query) == "" {
		writeJSONError(w, http.StatusBadRequest, "query is required")
		return
	}

	started := time.Now()
	out := doSearch(r.Context(), req.Query, req.Page-1)
	took := time.Since(started)

	if out.Err != nil && !errors.Is(out.Err, errAllFailed) {
		writeJSONError(w, http.StatusBadRequest, out.Err.Error())
		return
	}

	doc := buildDocument(out.Query, out.Results, out.Runs, req.MaxResults, took)

	status := http.StatusOK
	if errors.Is(out.Err, errAllFailed) {
		// Every engine failed: the document still explains what happened.
		status = http.StatusBadGateway
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := writeJSON(w, doc); err != nil {
		log.Printf("writing response failed: %v", err)
	}
}

// writeJSONError writes a small error body in the same shape as any other JSON
// response, so callers never have to parse two formats.
func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

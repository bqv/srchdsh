package engtest

import (
	"bytes"
	"compress/gzip"
	"encoding/gob"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"srchdsh/internal/brotlihack"
)

// mockTransport implements [net/http.RoundTripper] and intercepts all requests
// to either return the cached copy or to request a new copy for the cache.
type mockTransport struct {
	// Base directory to read/write files.
	//
	// If the directory does not exist, it will be created for you.
	Base string

	// Update will actually perform the requests and save the responses to
	// disk instead of simply mocking the response.
	Update bool
}

// RoundTrip handles requests to a HTTP server by either responding to them by
// looking at the cache or by actually requesting data from the server and
// caching it.
//
// This way, tests can be done deterministically without hammering the actual
// search engine in question.
func (ms *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if ms.Update {
		return ms.updateHandle(req)
	}
	return ms.mockHandle(req)
}

// Default mocking request handler.
// This reads the cache and returns the response.
func (ms *mockTransport) mockHandle(req *http.Request) (*http.Response, error) {
	res := &http.Response{
		Header: http.Header{},
	}

	r := getRequestInfo(req)
	h, err := os.Open(filepath.Join(ms.Base, hashuri(r.URL)))
	if err != nil {
		return nil, err
	}
	defer h.Close()

	rr := TestData{}
	if err := gob.NewDecoder(h).Decode(&rr); err != nil {
		return nil, err
	}
	resp := rr.Res

	res.Request = req
	res.StatusCode = resp.Status
	for _, v := range resp.Headers {
		res.Header.Add(v.Key, v.Value)
	}
	res.Body = io.NopCloser(bytes.NewReader(resp.Body))
	return res, nil
}

// Handles requests when Update mode is on.
func (ms *mockTransport) updateHandle(req *http.Request) (*http.Response, error) {
	r := getRequestInfo(req)

	// Perform the actual request
	client := &http.Client{
		Transport: http.DefaultTransport.(*http.Transport).Clone(),
	}
	client.Transport.(*http.Transport).DisableCompression = true

	// Perform the request.
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	// Grab the response and save it to disk.
	resp := getResponseInfo(res)
	save(ms.Base, r, resp)

	// Success!
	return res, nil
}

// Extract pertient information from the request to the server.
func getRequestInfo(ctx *http.Request) request {
	req := request{}
	req.URL = string(ctx.URL.String())
	req.Method = string(ctx.Method)

	// Copy most headers.
	for k, vals := range ctx.Header {
		for _, v := range vals {
			req.Headers = append(req.Headers, header{Key: k, Value: v})
		}
	}

	// Save the body.
	if ctx.Body != nil {
		var err error
		req.Body, err = io.ReadAll(ctx.Body)
		if err != nil {
			panic(err)
		}

		// Since we consumed the body, we need to reset the body again.
		// I *do not* like doing this here, but it is what it is.
		ctx.Body = io.NopCloser(bytes.NewReader(req.Body))
	}

	return req
}

// Extract pertient information from the response from the server.
func getResponseInfo(ctx *http.Response) response {
	res := response{}
	res.Status = ctx.StatusCode

	// Copy most headers.
	for k, vals := range ctx.Header {
		if k == "Content-Encoding" {
			// We automatically decompress it so this field does not matter
			continue
		}

		for _, v := range vals {
			res.Headers = append(res.Headers, header{Key: k, Value: v})
		}
	}

	// Decode.
	// This could probably be done better but it gets the job done.
	var body io.Reader
	var err error
	contentEncoding := ctx.Header.Values("Content-Encoding")
	if !ctx.Uncompressed && len(contentEncoding) > 0 {
		// net/http's default transport will decompress when it can,
		// but it needs some extra help for Brotli and gzip.

		switch contentEncoding[0] {
		case "br":
			body = brotlihack.NewReader(ctx.Body)
		case "gzip":
			body, err = gzip.NewReader(ctx.Body)
			if err != nil {
				panic(err)
			}
		default:
			// Unlikely since we support everything we request, but
			// it's there if we need it.
			panic("unknown content encoding: " + contentEncoding[0])
		}
	} else {
		// net/http has probably decompressed it on its own or the
		// server didn't compress its response at all.
		body = ctx.Body
	}

	res.Body, err = io.ReadAll(body)
	if err != nil {
		panic(err)
	}

	// Restore the body since we read it all.
	// Once again, I don't want to do this, but alas...
	ctx.Body = io.NopCloser(bytes.NewReader(res.Body))

	// Also drop Content-Encoding since we're dealing with it.
	ctx.Header.Del("Content-Encoding")

	return res
}

package search

import (
	"compress/gzip"
	"compress/zlib"
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

func initCompressionServer() *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/gzip", func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Encoding", "gzip")
		w := gzip.NewWriter(rw)
		w.Write([]byte("hello world gzip"))
		w.Close()
	})

	mux.HandleFunc("/br", func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Encoding", "br")
		w := brotli.NewWriter(rw)
		w.Write([]byte("hello world br"))
		w.Close()
	})

	mux.HandleFunc("/deflate", func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Encoding", "deflate")
		w := zlib.NewWriter(rw)
		w.Write([]byte("hello world deflate"))
		w.Close()
	})

	mux.HandleFunc("/zstd", func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Encoding", "zstd")
		w, err := zstd.NewWriter(rw)
		if err != nil {
			panic(err)
		}
		w.Write([]byte("hello world zstd"))
		w.Close()
	})

	return httptest.NewServer(mux)
}

// This function exists to prevent copying and pasting over 30 lines of code.
func testResponseDecompression(t *testing.T, method string) {
	srv := initCompressionServer()
	defer srv.Close()

	req, err := http.NewRequest("GET", srv.URL+"/"+method, nil)
	if err != nil {
		panic(err)
	}

	cli := srv.Client()

	// Change the transport used by the client to not bother with
	// compression
	cliTp := cli.Transport.(*http.Transport)
	cliTp.DisableCompression = true

	// Perform the request
	res, err := cli.Do(req)
	if err != nil {
		panic(err)
	}
	handleResponseDecompression(res)
	defer res.Body.Close()

	// Check for the expected value
	buf := &strings.Builder{}
	if _, err := io.Copy(buf, res.Body); err != nil {
		panic(err)
	}

	exp := "hello world " + method
	if buf.String() != exp {
		t.Errorf("expected %q, got %q", exp, buf.String())
	}
}

func TestResponseDecompressionGzip(t *testing.T) {
	testResponseDecompression(t, "gzip")
}

func TestResponseDecompressionBrotli(t *testing.T) {
	testResponseDecompression(t, "br")
}

func TestResponseDecompressionDeflate(t *testing.T) {
	testResponseDecompression(t, "deflate")
}

func TestResponseDecompressionZstd(t *testing.T) {
	testResponseDecompression(t, "zstd")
}

func TestHttpClientCookies(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	cookieChan := make(chan string, 1)
	defer close(cookieChan)

	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		for k, v := range r.Header {
			t.Logf("header %s: %v", k, v)
		}

		cookieChan <- r.Header.Get("Cookie")
	}))
	defer srv.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		panic(err)
	}

	cli := srv.Client()
	hc := &HttpClient{
		CookieJar: jar,
		http:      cli,
	}

	// Set a cookie
	surl, err := url.Parse(srv.URL)
	if err != nil {
		panic(err)
	}
	jar.SetCookies(surl, []*http.Cookie{
		&http.Cookie{Name: "test", Value: "hello, world!"},
	})

	t.Log(jar.Cookies(surl))

	// Build a request
	req, err := hc.New(ctx, "GET", srv.URL, nil)
	if err != nil {
		panic(err)
	}

	// Perform the request
	res, err := hc.Do(req)
	if err != nil {
		panic(err)
	}
	res.Body.Close()

	select {
	case <-time.After(time.Second):
		t.Fatalf("request timeout")
	case cookie := <-cookieChan:
		if cookie != `test="hello, world!"` {
			t.Errorf("expected %q, got %q", "hello, world!", cookie)
		}
	}
}

package search

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	nurl "net/url"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/klauspost/compress/zstd"

	"srchdsh/internal/brotlihack"
)

// HttpClient is a helpful wrapper around [net/http.Client] that does useful
// things to HTTP requests and responses you would've had to write anyway.
//
// The zero value is ready to use.
type HttpClient struct {
	// Timeout is the maximum amount of time to wait for the request to
	// complete.
	Timeout time.Duration

	// UserAgent holds the value of the User-Agent header of HTTP requests.
	//
	// If UserAgent is empty, then [DefaultUserAgent] is used.
	UserAgent string

	// Debug logs all HTTP requests sent through this HttpClient if it is
	// true before the first request is made.
	Debug bool

	// Send requests using this HTTP proxy.
	//
	// This does not default to the HTTP_PROXY environment variable and
	// must be explicitly set to use a proxy for all HTTP requests.
	HttpProxy string

	// Enable HTTP/3 using quic-go.
	QUIC bool

	// Enable zero roundtrip time for a performance boost on subsequent
	// connections.
	// Requires QUIC to be true.
	//
	// Using 0-RTT can have implications on the security of your connections as it
	// becomes possible to replay the data you send to the server.
	// Generally it is only safe to use it if the requests you are doing are
	// idempotent.
	// For srchd, this is always the case as of writing.
	//
	// For more information, refer to section 8 of RFC 8446:
	// https://datatracker.ietf.org/doc/html/rfc8446#section-8
	QUIC_0RTT bool

	// Specify a cookie jar to use.
	//
	// If left nil, no cookies will be saved.
	CookieJar http.CookieJar

	// If not set to nil, HTTP requests will use these headers as a base
	// instead of trying to emulate Chrome.
	//
	// The user agent is always set.
	BaseHeaders http.Header

	http *http.Client
	once sync.Once
}

// HttpError represents a generic HTTP error.
type HttpError struct {
	// Status code of response.
	Status int

	// URL of request.
	URL string

	// Method of request.
	Method string
}

// lazyReader is used to provide a [io.ReadCloser] which lazily initializes the
// decompresser reader.
type lazyReader struct {
	// Initialization function.
	init func(src io.Reader) (io.Reader, error)

	// Initialization error.
	// Once set, this never changes.
	err error

	// Decompresser stream.
	r io.Reader

	// Original response body.
	src io.ReadCloser
}

var (
	_ io.ReadCloser = &lazyReader{}
)

func (h HttpError) Error() string {
	return fmt.Sprintf("%s %q failed with status code %d", h.Method, h.URL, h.Status)
}

func newLazyReader(src io.ReadCloser, f func(src io.Reader) (io.Reader, error)) *lazyReader {
	return &lazyReader{init: f, src: src}
}

// Read reads data from the decompressor stream.
//
// Upon the first call to Read, the decompresser will be lazily initialized.
func (l *lazyReader) Read(data []byte) (int, error) {
	// Attempt to initialize the reader if necessary
	if l.r == nil && l.err == nil {
		l.r, l.err = l.init(l.src)
	}

	// If there was an initialization error, return it.
	if l.err != nil {
		return 0, l.err
	}

	return l.r.Read(data)
}

// Close closes the source stream of data.
func (l *lazyReader) Close() error {
	// In the case of zlib, a ReadCloser is returned.
	// This should be closed.
	if l.r != nil { // it's possible we never read
		if v, ok := l.r.(io.ReadCloser); ok {
			// Silently dropping the error is probably bad, but not
			// sure what else we could do here
			v.Close()
		}
	}

	return l.src.Close()
}

// Modifies the response object to use a decompresser as a reader if the
// response is compressed.
func handleResponseDecompression(res *http.Response) {
	// TODO: While nobody does this, it is techically possible for multiple
	// layers of compression to be applied onto a request.
	// `Content-Encoding: gzip, br` is perfectly valid and I hate it!

	// Unset content encoding and length.
	// The compiler inlines this.
	f := func() {
		res.ContentLength = -1
		res.Header.Del("Content-Encoding")
		res.Header.Del("Content-Length")
	}

	switch res.Header.Get("Content-Encoding") {
	case "gzip", "x-gzip":
		f()
		res.Body = newLazyReader(res.Body, func(src io.Reader) (io.Reader, error) {
			return gzip.NewReader(src)
		})
	case "br":
		f()
		res.Body = newLazyReader(res.Body, func(src io.Reader) (io.Reader, error) {
			return brotlihack.NewReader(src), nil
		})
	case "deflate":
		f()
		res.Body = newLazyReader(res.Body, func(src io.Reader) (io.Reader, error) {
			return zlib.NewReader(src)
		})
	case "zstd":
		f()
		res.Body = newLazyReader(res.Body, func(src io.Reader) (io.Reader, error) {
			return zstd.NewReader(src)
		})
	}
}

func setupTransport(h *HttpClient, hc *http.Client) {
	var proxy func(*http.Request) (*nurl.URL, error)

	if h.HttpProxy != "" {
		proxyURL, err := nurl.Parse(h.HttpProxy)
		if err != nil {
			panic(err) // TODO
		}

		proxy = http.ProxyURL(proxyURL)
	}

	// This is ripped from http.DefaultTransport
	hc.Transport = &http.Transport{
		Proxy:                 proxy,
		DialContext:           http.DefaultTransport.(*http.Transport).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DisableCompression:    true,
	}
}

func setupQUIC(h *HttpClient, hc *http.Client) {
	if !h.QUIC {
		return
	}

	// Use HTTP/3.
	rt := &http3.Transport{}
	if h.QUIC_0RTT {
		// Requires a little bit of extra
		// configuration for TLS.
		rt.TLSClientConfig = &tls.Config{
			ClientSessionCache: tls.NewLRUClientSessionCache(120),
		}
	}

	hc.Transport = rt
}

// Ensures that the HttpClient is ready to perform requests.
func (h *HttpClient) ensureReady() {
	h.once.Do(func() {
		// Create a new HTTP client.
		if h.http != nil {
			return
		}

		h.http = &http.Client{
			Timeout: h.Timeout,
			Jar:     h.CookieJar,
		}

		setupTransport(h, h.http)
		setupQUIC(h, h.http)

		// The debug flag requires us to use a different Transport than
		// the default one.
		/*
			if h.Debug {
				h.http.Transport = &debugRoundTripper{http.DefaultTransport}
			}
		*/
	})
}

// Creates a new context from a parent context.
func (h *HttpClient) Context(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, h.Timeout)
}

// Get a non-empty user agent.
func (h *HttpClient) ua() string {
	return h.UserAgent
}

// Client fetches the [net/http.Client] for this specific HTTP client.
//
// Do not change fields of the returned Client struct once you have performed a
// request.
func (h *HttpClient) Client() *http.Client {
	// This is a function because the HttpClient is lazily initialized.
	h.ensureReady()
	return h.http
}

// If using QUIC and 0-RTT, then GET/HEAD require special methods.
func (h *HttpClient) quicMethod(method string) string {
	if !h.QUIC || !h.QUIC_0RTT {
		// QUIC and 0RTT aren't both enabled at the same time so just
		// fallback to the usual methods.
		return method
	}

	switch method {
	case http.MethodGet:
		return http3.MethodGet0RTT
	case http.MethodHead:
		return http3.MethodHead0RTT
	default:
		return method
	}
}

// New creates a new HTTP request.
func (h *HttpClient) New(ctx context.Context, method, url string, body []byte, contentType ...string) (*http.Request, error) {
	h.ensureReady()

	// Parse the URL. We need this for cookies.
	parsedUrl, err := nurl.Parse(url)
	if err != nil {
		return nil, fmt.Errorf("failed to parse url: %w", err)
	}

	// We don't want to create a bytes.Reader on a nil body.
	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}

	// Initialize the request. This does a lot of the work for us.
	req, err := http.NewRequestWithContext(ctx, h.quicMethod(method), url, bodyReader)
	if err != nil {
		return nil, err
	}

	// NOTE: Unlike Fasthttp, I don't think the ordering of headers matters
	// all that much here, which is unfortunate because that was
	// essentially the only reason that I moved to it in the first place.
	// If I ever feel like reinventing the wheel, I could introduce some
	// mandatory ordering by reimplementing RoundTripper I think, but
	// that's a project for another day.

	req.Header.Set("User-Agent", h.ua())
	req.Header.Set("Host", parsedUrl.Host)

	if h.BaseHeaders == nil {
		for k, v := range defaultBaseHeaders {
			req.Header[k] = v
		}
	} else {
		for k, v := range h.BaseHeaders {
			req.Header[k] = v
		}
	}

	if body != nil {
		req.Header.Set("Content-Type", contentType[0])
		req.Header.Set("Content-Length", fmt.Sprint(len(body)))
	}

	// Add in our cookies.
	if h.CookieJar != nil {
		for _, v := range h.CookieJar.Cookies(parsedUrl) {
			req.AddCookie(v)
		}
	}

	return req, nil
}

func (h *HttpClient) Do(req *http.Request) (*http.Response, error) {
	if h.Debug {
		log.Printf("requesting %v", req.URL)
	}

	res, err := h.http.Do(req)

	// If we're using QUIC, then there's a possibility that the request
	// failed, but it was a temporary failure that could be immediately
	// retried.
	var tpErr *quic.TransportError
	if h.QUIC && errors.As(err, &tpErr) {
		if tpErr.ErrorCode == quic.NoError {
			// Retry the request.
			// XXX: Maybe it would be a good idea to count the
			// amount of times we go here, so we don't hammer the
			// server with requests if it keeps telling us
			// NO_ERROR.
			return h.Do(req)
		}

		// This space is intentionally left blank
	}

	if err == nil {
		handleResponseDecompression(res)
	}

	return res, err
}

// Get performs a GET request on a given URL.
//
// If the server responds with a non-200 status code, then the returned
// response will be nil and err will be of type [HttpError].
func (h *HttpClient) Get(ctx context.Context, url string) (*http.Response, error) {
	h.ensureReady()

	// Create a request.
	req, err := h.New(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Perform the request.
	res, err := h.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to perform request: %w", err)
	}

	// Error out on non-200 status codes.
	if res.StatusCode != 200 {
		// The request itself succeeded but we aren't interested in
		// anything we got due to the failure status.
		return nil, HttpError{Status: res.StatusCode, URL: url, Method: "GET"}
	}

	// All good.
	return res, nil
}

// Post performs a POST request on a given URL.
//
// If the server responds with a non-200 status code, then the returned
// response will be nil and err will be of type [HttpError].
func (h *HttpClient) Post(ctx context.Context, url string, contentType string, body []byte) (*http.Response, error) {
	h.ensureReady()

	// Create a request.
	req, err := h.New(ctx, http.MethodPost, url, body, contentType)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Perform the request.
	res, err := h.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to perform request: %w", err)
	}

	// Error out on non-200 status codes.
	if res.StatusCode != 200 {
		// The request itself succeeded but we aren't interested in
		// anything we got due to the failure status.
		if h.Debug {
			log.Printf("post %s failed with status code %d", url, res.StatusCode)
		}

		return nil, HttpError{Status: res.StatusCode, URL: url, Method: "POST"}
	}

	// All good.
	return res, nil
}

// Shared code between HtmlGet and HtmlPost.
func DocumentFromHttpResponse(res *http.Response) (*goquery.Document, error) {
	var err error

	// Decode.
	// This could probably be done better but it gets the job done.
	var body io.Reader
	contentEncoding := res.Header.Values("Content-Encoding")
	if !res.Uncompressed && len(contentEncoding) > 0 {
		// net/http's default transport will decompress when it can,
		// but it needs some extra help for Brotli and gzip.

		switch contentEncoding[0] {
		case "br":
			body = brotlihack.NewReader(res.Body)
		case "gzip":
			body, err = gzip.NewReader(res.Body)
			if err != nil {
				return nil, err
			}
		default:
			// Unlikely since we support everything we request, but
			// it's there if we need it.
			return nil, fmt.Errorf("unknown content encoding: %v", contentEncoding)
		}
	} else {
		// net/http has probably decompressed it on its own or the
		// server didn't compress its response at all.
		body = res.Body
	}

	doc, err := goquery.NewDocumentFromReader(body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse html: %w", err)
	}
	return doc, nil
}

// Helper function to fetch HTML using a GET request and automatically parse
// it.
//
// If the server responds with a non-200 status code, then the returned
// response will be nil and err will be of type [HttpError].
//
// The returned response will have a closed body.
func (h *HttpClient) HtmlGet(ctx context.Context, url string) (*http.Response, *goquery.Document, error) {
	// Fire off a request.
	res, err := h.Get(ctx, url)
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()

	doc, err := DocumentFromHttpResponse(res)
	return res, doc, err
}

// Helper function to fetch HTML using a GET request and automatically parse
// it.
//
// If the server responds with a non-200 status code, then the returned
// response will be nil and err will be of type [HttpError].
//
// The returned response will have a closed body.
func (h *HttpClient) HtmlPost(ctx context.Context, url string, contentType string, body []byte) (*http.Response, *goquery.Document, error) {
	// Fire off a request.
	res, err := h.Post(ctx, url, contentType, body)
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()

	doc, err := DocumentFromHttpResponse(res)
	return res, doc, err
}

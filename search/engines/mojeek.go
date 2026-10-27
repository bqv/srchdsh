package engines

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/crypto/pbkdf2"

	"srchdsh/internal/cache"
	"srchdsh/search"
)

// Mojeek runs its own crawler, so its results are an independent index rather
// than a reskin of Google or Bing. Automated querying is gated behind an
// Altcha proof of work: solve it once, keep the chllg cookie, and search
// normally.
//
// Note that https://www.mojeek.com/robots.txt disallows /search for all user
// agents. This engine scrapes it anyway, by explicit decision of the owner of
// this fork, who preferred it to paying for Mojeek's search API. Do not copy
// this engine into a tree that has not made the same decision.
const (
	mojeekURL          = "https://www.mojeek.com"
	mojeekSearchURL    = mojeekURL + "/search"
	mojeekChallengeURL = mojeekURL + "/captcha/challenge"
	mojeekVerifyURL    = mojeekURL + "/captcha/verify"

	// mojeekAltchaAlgorithm is the only derivation we know how to solve.
	mojeekAltchaAlgorithm = "PBKDF2/SHA-256"

	// mojeekMaxCounter bounds the search for the challenge's hidden counter.
	// Observed answers are in the low hundreds; a challenge that is not
	// solved by then is treated as a wall.
	mojeekMaxCounter = 2000

	// mojeekCookieTTL is the fallback lifetime of the chllg cookie when the
	// server does not state one. The cookie normally lasts about a month.
	mojeekCookieTTL = 24 * time.Hour

	// mojeekCookieName is the cookie holding the solved-challenge token.
	mojeekCookieName = "chllg"
)

type mojeek struct {
	name  string
	http  *search.HttpClient
	cache *cache.Store
}

var (
	_ search.Engine = &mojeek{}
)

func init() {
	search.Add("mojeek", true, func(config search.Config) (search.Engine, error) {
		cli := config.NewHttpClient()
		if cli.CookieJar == nil {
			jar, err := cookiejar.New(nil)
			if err != nil {
				return nil, fmt.Errorf("mojeek: create cookie jar: %w", err)
			}
			cli.CookieJar = jar
		}

		return &mojeek{
			name:  config.Name,
			http:  cli,
			cache: cache.Open("mojeek"),
		}, nil
	})
}

// Search queries Mojeek and returns the web results.
func (m *mojeek) Search(ctx context.Context, query string, page int) ([]search.Result, error) {
	ctx, cancel := m.http.Context(ctx)
	defer cancel()

	m.restoreCookie()

	// Mojeek expects the solved-challenge cookie to be present rather than
	// answering with a challenge page, so make sure there is one before
	// asking. A cookie the server no longer accepts still gets caught below.
	if !m.hasCookie() {
		if err := m.solve(ctx); err != nil {
			return nil, err
		}
	}

	body, err := m.search(ctx, query, page)
	if err != nil {
		return nil, err
	}

	if isAltchaChallenge(body) {
		if err := m.solve(ctx); err != nil {
			return nil, err
		}
		if body, err = m.search(ctx, query, page); err != nil {
			return nil, err
		}
	}

	return parseMojeek(body, m.name)
}

// Ping checks that Mojeek is reachable.
func (m *mojeek) Ping(ctx context.Context) error {
	ctx, cancel := m.http.Context(ctx)
	defer cancel()

	req, err := m.http.New(ctx, http.MethodGet, mojeekURL+"/", nil)
	if err != nil {
		return fmt.Errorf("mojeek: create request: %w", err)
	}
	res, err := m.http.Do(req)
	if err != nil {
		return fmt.Errorf("mojeek: %w", err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)

	if res.StatusCode != http.StatusOK {
		return search.HttpError{Status: res.StatusCode, URL: req.URL.String(), Method: req.Method}
	}
	return nil
}

// hasCookie reports whether the jar holds a challenge cookie.
func (m *mojeek) hasCookie() bool {
	u, err := url.Parse(mojeekURL + "/")
	if err != nil {
		return false
	}

	for _, c := range m.http.CookieJar.Cookies(u) {
		if c.Name == mojeekCookieName && c.Value != "" {
			return true
		}
	}
	return false
}

// restoreCookie puts the cached chllg cookie back into the jar, so a fresh
// process does not pay for a proof of work the month has already covered.
func (m *mojeek) restoreCookie() {
	var token string
	if !m.cache.Get(mojeekCookieName, &token) || token == "" {
		return
	}

	u, err := url.Parse(mojeekURL + "/")
	if err != nil {
		return
	}
	m.http.CookieJar.SetCookies(u, []*http.Cookie{{
		Name:  mojeekCookieName,
		Value: token,
		Path:  "/",
	}})
}

// search performs one query and returns the response body.
func (m *mojeek) search(ctx context.Context, query string, page int) ([]byte, error) {
	q := url.Values{}
	q.Set("q", query)
	// Search settings: 0 disables safe search, and the page offset counts
	// results, not pages.
	q.Set("safe", "0")
	if page > 0 {
		// Mojeek links page two as s=11, page three as s=21: the
		// offset is one-based, so page n starts at 10n+1.
		q.Set("s", strconv.Itoa(10*page+1))
	}

	req, err := m.http.New(ctx, http.MethodGet, mojeekSearchURL+"?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("mojeek: create request: %w", err)
	}
	req.Header.Set("Referer", mojeekURL+"/")

	res, err := m.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mojeek: %w", err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("mojeek: read response: %w", err)
	}

	// A rejected cookie comes back as another challenge rather than an
	// error status, so the body is checked by the caller.
	if res.StatusCode == http.StatusForbidden {
		if bytes.Contains(body, []byte("automated queries")) {
			return nil, fmt.Errorf("%w: mojeek refused the request as automated", search.ErrCaptcha)
		}
		return nil, fmt.Errorf("%w: mojeek returned 403", search.ErrCaptcha)
	}
	if res.StatusCode != http.StatusOK {
		return nil, search.HttpError{Status: res.StatusCode, URL: req.URL.String(), Method: req.Method}
	}

	return body, nil
}

// altchaChallenge is the payload of Mojeek's proof of work.
type altchaChallenge struct {
	Parameters struct {
		Algorithm    string `json:"algorithm"`
		Cost         int    `json:"cost"`
		KeyLength    int    `json:"keyLength"`
		KeyPrefix    string `json:"keyPrefix"`
		Nonce        string `json:"nonce"`
		Salt         string `json:"salt"`
		KeySignature string `json:"keySignature"`
		ExpiresAt    int64  `json:"expiresAt"`
	} `json:"parameters"`
	Signature string `json:"signature"`
}

// isAltchaChallenge reports whether body is the captcha page rather than
// results.
func isAltchaChallenge(body []byte) bool {
	return bytes.Contains(body, []byte("altcha"))
}

// solveAltcha finds the counter whose PBKDF2 output starts with the
// challenge's key prefix.
//
// The prefix is the full derived key for the server's hidden counter, which is
// small (in the low hundreds in practice). Enumerating counters therefore
// finds it quickly; the long prefix is what makes a wrong answer unambiguous,
// not what makes the search hard.
func solveAltcha(ctx context.Context, p altchaChallenge) (int, string, error) {
	if p.Parameters.Algorithm != mojeekAltchaAlgorithm {
		return 0, "", fmt.Errorf("%w: unsupported altcha algorithm %q", search.ErrCaptcha, p.Parameters.Algorithm)
	}
	if p.Parameters.Cost <= 0 || p.Parameters.KeyLength <= 0 {
		return 0, "", fmt.Errorf("%w: malformed altcha parameters", search.ErrCaptcha)
	}

	nonce, err := hex.DecodeString(p.Parameters.Nonce)
	if err != nil {
		return 0, "", fmt.Errorf("%w: altcha nonce: %v", search.ErrCaptcha, err)
	}
	salt, err := hex.DecodeString(p.Parameters.Salt)
	if err != nil {
		return 0, "", fmt.Errorf("%w: altcha salt: %v", search.ErrCaptcha, err)
	}
	prefix, err := hex.DecodeString(p.Parameters.KeyPrefix)
	if err != nil || len(prefix) == 0 {
		return 0, "", fmt.Errorf("%w: altcha key prefix", search.ErrCaptcha)
	}

	secret := make([]byte, len(nonce)+4)
	copy(secret, nonce)

	for counter := 0; counter < mojeekMaxCounter; counter++ {
		if counter&0x3f == 0 {
			if err := ctx.Err(); err != nil {
				return 0, "", err
			}
		}

		// The counter is appended as a big-endian 32-bit integer.
		binary.BigEndian.PutUint32(secret[len(nonce):], uint32(counter))

		key := pbkdf2.Key(secret, salt, p.Parameters.Cost, p.Parameters.KeyLength, sha256.New)
		if bytes.HasPrefix(key, prefix) {
			return counter, hex.EncodeToString(key), nil
		}
	}

	return 0, "", fmt.Errorf("%w: altcha proof of work exhausted", search.ErrCaptcha)
}

// solve runs the full challenge dance: fetch, solve, submit, keep the cookie.
func (m *mojeek) solve(ctx context.Context) error {
	req, err := m.http.New(ctx, http.MethodGet, mojeekChallengeURL, nil)
	if err != nil {
		return fmt.Errorf("mojeek: create challenge request: %w", err)
	}
	req.Header.Set("Referer", mojeekURL+"/")

	res, err := m.http.Do(req)
	if err != nil {
		return fmt.Errorf("mojeek: fetch challenge: %w", err)
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("mojeek: read challenge: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: challenge returned %d", search.ErrCaptcha, res.StatusCode)
	}

	var challenge altchaChallenge
	if err := json.Unmarshal(raw, &challenge); err != nil {
		return fmt.Errorf("%w: decode challenge: %v", search.ErrCaptcha, err)
	}

	counter, derived, err := solveAltcha(ctx, challenge)
	if err != nil {
		return err
	}

	payload, err := json.Marshal(struct {
		Challenge altchaChallenge `json:"challenge"`
		Solution  struct {
			Counter    int    `json:"counter"`
			DerivedKey string `json:"derivedKey"`
			Time       int    `json:"time"`
		} `json:"solution"`
	}{
		Challenge: challenge,
		Solution: struct {
			Counter    int    `json:"counter"`
			DerivedKey string `json:"derivedKey"`
			Time       int    `json:"time"`
		}{Counter: counter, DerivedKey: derived, Time: 100 + rand.IntN(100)},
	})
	if err != nil {
		return fmt.Errorf("mojeek: encode solution: %w", err)
	}

	var form bytes.Buffer
	w := multipart.NewWriter(&form)
	part, err := w.CreateFormField("altcha")
	if err != nil {
		return fmt.Errorf("mojeek: encode form: %w", err)
	}
	if _, err := part.Write([]byte(base64.StdEncoding.EncodeToString(payload))); err != nil {
		return fmt.Errorf("mojeek: encode form: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("mojeek: encode form: %w", err)
	}

	req, err = m.http.New(ctx, http.MethodPost, mojeekVerifyURL, form.Bytes(), w.FormDataContentType())
	if err != nil {
		return fmt.Errorf("mojeek: create verify request: %w", err)
	}
	req.Header.Set("Referer", mojeekURL+"/")

	res, err = m.http.Do(req)
	if err != nil {
		return fmt.Errorf("mojeek: submit challenge: %w", err)
	}
	defer res.Body.Close()

	reply, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: challenge rejected (%d): %s", search.ErrCaptcha, res.StatusCode, strings.TrimSpace(string(reply)))
	}

	token, ttl := responseCookie(res, mojeekCookieName, mojeekCookieTTL)
	if token == "" {
		return fmt.Errorf("%w: no %s cookie in challenge response", search.ErrCaptcha, mojeekCookieName)
	}

	_ = m.cache.Put(mojeekCookieName, token, ttl)
	return nil
}

// parseMojeek extracts the web results from a results page.
func parseMojeek(body []byte, name string) ([]search.Result, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("mojeek: parse page: %w", err)
	}

	items := doc.Find("ul.results-standard > li")
	if items.Length() == 0 {
		return nil, fmt.Errorf("%w: no results list in mojeek response", search.ErrCaptcha)
	}

	results := make([]search.Result, 0, items.Length())
	items.Each(func(_ int, item *goquery.Selection) {
		link, ok := item.Find("h2 > a.title").First().Attr("href")
		if !ok || link == "" {
			return
		}

		results = append(results, search.Result{
			Title:       strings.TrimSpace(item.Find("h2 > a.title").First().Text()),
			Description: strings.TrimSpace(item.Find("p.s").First().Text()),
			Link:        cleanResultURL(link),
			Sources:     []string{name},
		})
	})

	return results, nil
}

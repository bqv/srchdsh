package engines

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"git.sr.ht/~cmcevoy/srchd/internal/cache"
	"git.sr.ht/~cmcevoy/srchd/search"
)

// Startpage serves Google's index by proxy, behind an Anubis proof-of-work
// challenge. A solved challenge yields an spchal-auth cookie which is valid for
// five minutes; the cookie is cached so a burst of queries pays for only one
// proof of work.
const (
	startpageURL       = "https://www.startpage.com"
	startpageSearchURL = startpageURL + "/sp/search"
	startpagePassURL   = startpageURL + "/.within.website/x/cmd/anubis/api/pass-challenge"

	// startpageSERPStart marks the JSON blob embedded in a results page.
	// The page is built with React and the payload sits between this and
	// the matching "}})".
	startpageSERPStart = "React.createElement(UIStartpage.AppSerpWeb, {"

	// maxAnubisDifficulty is the highest difficulty we are willing to
	// solve. Startpage has used 4; anything higher is treated as a captcha
	// wall rather than burning the request timeout on hashes.
	maxAnubisDifficulty = 4

	// scTTL is how long a scraped sc form token is reused. Startpage treats
	// a request without a fresh sc as a bot.
	scTTL = time.Hour

	// spchalTTL is the fallback lifetime of the Anubis cookie when the
	// server does not state an expiry. The cookie normally lasts five
	// minutes.
	spchalTTL = 4 * time.Minute
)

// startpagePreferences is the Startpage settings cookie. It selects the POST
// search form, English, ten results per page and no safe search, matching the
// form a browser would submit.
const startpagePreferences = "date_timeEEEworldN1Ndisable_family_filterEEEnoneN1Ndisable_open_in_new_windowEEE0N1N" +
	"enable_post_methodEEE1N1Nenable_proxy_safety_suggestEEE1N1Nenable_stay_controlEEE1N1N" +
	"instant_answersEEE1N1Nlang_homepageEEEs/device/en/N1Nnum_of_resultsEEE10N1N" +
	"suggestionsEEE1N1Nwt_unitEEEcelsius"

var (
	// startpageAgoPrefix matches the relative date Startpage puts in front
	// of some snippets, e.g. "17 hours ago ... ".
	startpageAgoPrefix = regexp.MustCompile(`^(?:[0-9]+ (?:second|minute|hour|day|week|month|year)s? ago|[0-9]{1,2} [A-Z][a-z]{2} [0-9]{4}) \.\.\.\s*`)
)

type startpage struct {
	name  string
	http  *search.HttpClient
	cache *cache.Store
}

var (
	_ search.Engine = &startpage{}
)

func init() {
	search.Add("startpage", true, func(config search.Config) (search.Engine, error) {
		cli := config.NewHttpClient()
		if cli.CookieJar == nil {
			jar, err := cookiejar.New(nil)
			if err != nil {
				return nil, fmt.Errorf("startpage: create cookie jar: %w", err)
			}
			cli.CookieJar = jar
		}

		return &startpage{
			name:  config.Name,
			http:  cli,
			cache: cache.Open("startpage"),
		}, nil
	})
}

// Search queries Startpage and returns the web results.
func (s *startpage) Search(ctx context.Context, query string, page int) ([]search.Result, error) {
	ctx, cancel := s.http.Context(ctx)
	defer cancel()

	s.restoreCookie()

	sc, err := s.scCode(ctx)
	if err != nil {
		return nil, err
	}

	body, err := s.search(ctx, query, page, sc)
	if err != nil {
		return nil, err
	}

	// The very first request in a while is answered with a challenge
	// instead of results. Solve it and ask once more.
	if isAnubisChallenge(body) {
		if err := s.passAnubis(ctx, body); err != nil {
			return nil, err
		}
		if body, err = s.search(ctx, query, page, sc); err != nil {
			return nil, err
		}
	}

	return parseStartpage(body, s.name)
}

// Ping checks that Startpage is reachable.
func (s *startpage) Ping(ctx context.Context) error {
	ctx, cancel := s.http.Context(ctx)
	defer cancel()

	s.restoreCookie()

	_, err := s.homepage(ctx)
	return err
}

// restoreCookie puts a cached Anubis pass cookie back into the jar. Without
// this every process would pay for a proof of work even though the cookie is
// still valid.
func (s *startpage) restoreCookie() {
	var token string
	if !s.cache.Get("spchal-auth", &token) || token == "" {
		return
	}

	u, err := url.Parse(startpageURL + "/")
	if err != nil {
		return
	}

	// The cookie is set for the whole of startpage.com.
	s.http.CookieJar.SetCookies(u, []*http.Cookie{{
		Name:   "spchal-auth",
		Value:  token,
		Path:   "/",
		Domain: "startpage.com",
		Secure: true,
	}})
}

// scCode returns a fresh sc form token, from the cache when possible.
func (s *startpage) scCode(ctx context.Context) (string, error) {
	var sc string
	if s.cache.Get("sc", &sc) && sc != "" {
		return sc, nil
	}

	body, err := s.homepage(ctx)
	if err != nil {
		return "", err
	}

	sc, err = parseSC(body)
	if err != nil {
		return "", err
	}

	// Failing to persist costs a request next time, nothing more.
	_ = s.cache.Put("sc", sc, scTTL)
	return sc, nil
}

// homepage fetches the start page, solving a challenge if one is served.
func (s *startpage) homepage(ctx context.Context) ([]byte, error) {
	body, err := s.get(ctx, startpageURL+"/")
	if err != nil {
		return nil, err
	}

	if isAnubisChallenge(body) {
		if err := s.passAnubis(ctx, body); err != nil {
			return nil, err
		}
		return s.get(ctx, startpageURL+"/")
	}

	return body, nil
}

// search submits the search form and returns the response body.
func (s *startpage) search(ctx context.Context, query string, page int, sc string) ([]byte, error) {
	form := url.Values{}
	form.Set("query", query)
	form.Set("cat", "web")
	form.Set("t", "device")
	form.Set("sc", sc)
	form.Set("with_date", "")
	form.Set("abd", "1")
	form.Set("abe", "1")
	form.Set("qsr", "all")
	form.Set("qadf", "none")
	form.Set("language", "english")
	form.Set("lui", "english")
	form.Set("segment", "startpage.udog")
	if page > 0 {
		// srchd counts pages from zero; Startpage counts from one.
		form.Set("page", strconv.Itoa(page+1))
	}

	req, err := s.http.New(ctx, http.MethodPost, startpageSearchURL, []byte(form.Encode()),
		"application/x-www-form-urlencoded")
	if err != nil {
		return nil, fmt.Errorf("startpage: create request: %w", err)
	}
	req.Header.Set("Origin", startpageURL)
	req.Header.Set("Referer", startpageURL+"/")
	req.AddCookie(&http.Cookie{Name: "preferences", Value: startpagePreferences})

	return s.do(req)
}

// get performs a GET request and returns the body.
func (s *startpage) get(ctx context.Context, address string) ([]byte, error) {
	req, err := s.http.New(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, fmt.Errorf("startpage: create request: %w", err)
	}

	return s.do(req)
}

// do performs req, returning the body of a successful response.
func (s *startpage) do(req *http.Request) ([]byte, error) {
	res, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("startpage: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, search.HttpError{Status: res.StatusCode, URL: req.URL.String(), Method: req.Method}
	}

	// A redirect to the captcha page is a refusal, not a result page.
	if strings.HasPrefix(res.Request.URL.Path, "/sp/captcha") {
		return nil, fmt.Errorf("%w: startpage redirected to /sp/captcha", search.ErrCaptcha)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("startpage: read response: %w", err)
	}
	return body, nil
}

// anubisChallenge is the payload of the challenge page.
type anubisChallenge struct {
	Rules struct {
		Algorithm  string `json:"algorithm"`
		Difficulty int    `json:"difficulty"`
	} `json:"rules"`
	Challenge struct {
		ID         string `json:"id"`
		RandomData string `json:"randomData"`
	} `json:"challenge"`
}

// isAnubisChallenge reports whether body is a challenge page rather than
// results.
func isAnubisChallenge(body []byte) bool {
	return bytes.Contains(body, []byte(`id="anubis_challenge"`))
}

// parseAnubisChallenge extracts the challenge from a challenge page.
func parseAnubisChallenge(body []byte) (*anubisChallenge, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("startpage: parse challenge page: %w", err)
	}

	raw := strings.TrimSpace(doc.Find("script#anubis_challenge").First().Text())
	if raw == "" {
		return nil, fmt.Errorf("%w: no anubis challenge in page", search.ErrCaptcha)
	}

	var c anubisChallenge
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return nil, fmt.Errorf("startpage: decode anubis challenge: %w", err)
	}
	if c.Challenge.ID == "" || c.Challenge.RandomData == "" {
		return nil, fmt.Errorf("%w: incomplete anubis challenge", search.ErrCaptcha)
	}
	return &c, nil
}

// solveAnubis finds a nonce such that sha256(randomData + nonce) starts with
// difficulty hexadecimal zeroes.
func solveAnubis(ctx context.Context, randomData string, difficulty int) (int, string, error) {
	if difficulty < 1 || difficulty > maxAnubisDifficulty {
		return 0, "", fmt.Errorf("%w: anubis difficulty %d unsupported", search.ErrCaptcha, difficulty)
	}

	prefix := strings.Repeat("0", difficulty)
	// The average search is 16^difficulty/2; allow eight times that, which
	// fails with a probability of about 0.03%.
	limit := 8 << (4 * difficulty)

	blob := []byte(randomData)
	buf := make([]byte, 0, len(blob)+20)

	for n := 0; n < limit; n++ {
		if n&0xfff == 0 {
			if err := ctx.Err(); err != nil {
				return 0, "", err
			}
		}

		buf = append(buf[:0], blob...)
		buf = strconv.AppendInt(buf, int64(n), 10)

		sum := sha256.Sum256(buf)
		digest := hex.EncodeToString(sum[:])
		if strings.HasPrefix(digest, prefix) {
			return n, digest, nil
		}
	}

	return 0, "", fmt.Errorf("%w: anubis proof of work exhausted", search.ErrCaptcha)
}

// passAnubis solves the challenge in body, submits the answer and stores the
// resulting exemption cookie.
func (s *startpage) passAnubis(ctx context.Context, body []byte) error {
	c, err := parseAnubisChallenge(body)
	if err != nil {
		return err
	}

	nonce, digest, err := solveAnubis(ctx, c.Challenge.RandomData, c.Rules.Difficulty)
	if err != nil {
		return err
	}

	q := url.Values{}
	q.Set("id", c.Challenge.ID)
	q.Set("response", digest)
	q.Set("nonce", strconv.Itoa(nonce))
	q.Set("redir", startpageSearchURL)
	q.Set("elapsedTime", "1")

	req, err := s.http.New(ctx, http.MethodGet, startpagePassURL+"?"+q.Encode(), nil)
	if err != nil {
		return fmt.Errorf("startpage: create challenge request: %w", err)
	}

	// The answer is a redirect carrying Set-Cookie. Following it would hide
	// the cookie from us on the final response.
	cli := *s.http.Client()
	cli.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}

	res, err := cli.Do(req)
	if err != nil {
		return fmt.Errorf("startpage: submit challenge: %w", err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)

	cookie, ttl := responseCookie(res, "spchal-auth")
	if cookie == "" {
		return fmt.Errorf("%w: no spchal-auth cookie in challenge response", search.ErrCaptcha)
	}

	_ = s.cache.Put("spchal-auth", cookie, ttl)
	return nil
}

// responseCookie finds a cookie in a response and returns its value along with
// how long it will last.
func responseCookie(res *http.Response, name string) (string, time.Duration) {
	for _, c := range res.Cookies() {
		if c.Name != name {
			continue
		}
		if c.MaxAge > 0 {
			return c.Value, time.Duration(c.MaxAge) * time.Second
		}
		if !c.Expires.IsZero() {
			if ttl := time.Until(c.Expires); ttl > 0 {
				return c.Value, ttl
			}
		}
		return c.Value, spchalTTL
	}
	return "", 0
}

// parseSC pulls the sc form token out of a start page.
func parseSC(body []byte) (string, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("startpage: parse start page: %w", err)
	}

	sc, ok := doc.Find(`form#search input[name="sc"]`).First().Attr("value")
	if !ok || sc == "" {
		return "", fmt.Errorf("%w: no sc token on start page", search.ErrCaptcha)
	}
	return sc, nil
}

// startpageSERP is the subset of the embedded React payload we need.
type startpageSERP struct {
	Render struct {
		Presenter struct {
			Regions struct {
				Mainline []struct {
					DisplayType string `json:"display_type"`
					Results     []struct {
						ClickURL          string `json:"clickUrl"`
						Title             string `json:"title"`
						TitleNoFormatting string `json:"titleNoFormatting"`
						Description       string `json:"description"`
					} `json:"results"`
				} `json:"mainline"`
			} `json:"regions"`
		} `json:"presenter"`
	} `json:"render"`
}

// parseStartpage extracts the web results from a results page.
func parseStartpage(body []byte, name string) ([]search.Result, error) {
	idx := bytes.Index(body, []byte(startpageSERPStart))
	if idx < 0 {
		return nil, fmt.Errorf("%w: no results payload in startpage response", search.ErrCaptcha)
	}

	rest := body[idx+len(startpageSERPStart):]
	end := bytes.Index(rest, []byte("}})"))
	if end < 0 {
		return nil, errors.New("startpage: malformed results payload")
	}

	var page startpageSERP
	if err := json.Unmarshal([]byte("{"+string(rest[:end])+"}}"), &page); err != nil {
		return nil, fmt.Errorf("startpage: decode results payload: %w", err)
	}

	var results []search.Result
	for _, region := range page.Render.Presenter.Regions.Mainline {
		if region.DisplayType != "web-google" {
			continue
		}

		for _, r := range region.Results {
			if r.ClickURL == "" {
				continue
			}

			title := r.Title
			if r.TitleNoFormatting != "" {
				title = r.TitleNoFormatting
			}

			results = append(results, search.Result{
				Title: htmlText(title),
				// The date is stripped after converting to text:
				// Startpage wraps the ellipsis after it in markup, so
				// the prefix does not match the raw HTML.
				Description: startpageAgoPrefix.ReplaceAllString(htmlText(r.Description), ""),
				Link:        cleanStartpageURL(r.ClickURL),
				Sources:     []string{name},
			})
		}
	}

	return results, nil
}

// cleanStartpageURL removes tracking parameters from a result link.
//
// [search.CleanURL] strips one instance of each parameter pattern per call, so
// repeat until the URL settles: Startpage links often carry several utm_
// parameters.
func cleanStartpageURL(link string) string {
	for i := 0; i < 8; i++ {
		cleaned := search.CleanURL(link)
		if cleaned == link {
			break
		}
		link = cleaned
	}
	return link
}

// htmlText converts a fragment of HTML into plain text.
func htmlText(v string) string {
	if v == "" {
		return ""
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<p>" + v + "</p>"))
	if err != nil {
		return strings.TrimSpace(v)
	}
	return strings.TrimSpace(doc.Text())
}

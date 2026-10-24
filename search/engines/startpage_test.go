package engines

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"testing"

	"git.sr.ht/~cmcevoy/srchd/search"
)

func TestSolveAnubis(t *testing.T) {
	const (
		randomData = "6c1f3a9d8e5b"
		difficulty = 3
	)

	nonce, digest, err := solveAnubis(context.Background(), randomData, difficulty)
	if err != nil {
		t.Fatalf("solve: %v", err)
	}

	if !strings.HasPrefix(digest, "000") {
		t.Fatalf("digest %q does not start with three zeroes", digest)
	}

	sum := sha256.Sum256([]byte(randomData + strconv.Itoa(nonce)))
	if got := hex.EncodeToString(sum[:]); got != digest {
		t.Fatalf("digest %q does not match hash of nonce %d (%q)", digest, nonce, got)
	}
}

func TestSolveAnubisDifficultyTooHigh(t *testing.T) {
	_, _, err := solveAnubis(context.Background(), "abc", maxAnubisDifficulty+1)
	if !errors.Is(err, search.ErrCaptcha) {
		t.Fatalf("got %v, wanted ErrCaptcha", err)
	}
}

func TestSolveAnubisCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Difficulty 4 means the answer is not found before the first context
	// check unless the nonce happens to be tiny, so cancellation must win.
	if _, _, err := solveAnubis(ctx, "abc", 4); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, wanted context.Canceled", err)
	}
}

func TestParseAnubisChallenge(t *testing.T) {
	body := []byte(`<html><body><h1>Making sure you're not a bot!</h1>` +
		`<script id="anubis_version" type="application/json">"v1.26.4"</script>` +
		`<script id="anubis_challenge" type="application/json">` +
		`{"rules":{"algorithm":"fast","difficulty":4},` +
		`"challenge":{"issuedAt":"2026-10-24T20:44:24.401277771Z",` +
		`"id":"01a16fa8-51d1-7438-8f77-a9d56aa9e19e","randomData":"6c1f3a9d8e5b"}}</script>` +
		`</body></html>`)

	c, err := parseAnubisChallenge(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if c.Rules.Difficulty != 4 {
		t.Fatalf("difficulty = %d, wanted 4", c.Rules.Difficulty)
	}
	if c.Challenge.ID != "01a16fa8-51d1-7438-8f77-a9d56aa9e19e" {
		t.Fatalf("id = %q", c.Challenge.ID)
	}
	if c.Challenge.RandomData != "6c1f3a9d8e5b" {
		t.Fatalf("randomData = %q", c.Challenge.RandomData)
	}
}

func TestParseAnubisChallengeMissing(t *testing.T) {
	_, err := parseAnubisChallenge([]byte(`<html><body>no challenge here</body></html>`))
	if !errors.Is(err, search.ErrCaptcha) {
		t.Fatalf("got %v, wanted ErrCaptcha", err)
	}
}

func TestParseSC(t *testing.T) {
	body := []byte(`<html><body><form id="search" action="/sp/search" method="post">` +
		`<input type="hidden" name="sc" value="gQhKyhqUtbYECofxqm59CvPRznDzblDVNC6ngDj5s4q7"/>` +
		`<input type="text" name="query"></form></body></html>`)

	sc, err := parseSC(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if sc != "gQhKyhqUtbYECofxqm59CvPRznDzblDVNC6ngDj5s4q7" {
		t.Fatalf("sc = %q", sc)
	}
}

func TestParseSCMissing(t *testing.T) {
	_, err := parseSC([]byte(`<html><body><form id="search"></form></body></html>`))
	if !errors.Is(err, search.ErrCaptcha) {
		t.Fatalf("got %v, wanted ErrCaptcha", err)
	}
}

// startpageSERPBody builds a response body shaped like a real results page: the
// marker, then the serialized props with the last two braces and the call's
// closing parenthesis following. A live page splits its JSON the same way,
// which is why the payload is trimmed rather than embedded as a whole object.
func startpageSERPBody(payload string) []byte {
	return []byte(startpageSERPStart + payload[1:len(payload)-2] + "}})")
}

func TestParseStartpage(t *testing.T) {
	payload := `{"render":{"presenter":{"regions":{"mainline":[` +
		`{"display_type":"ads-google-top","results":[{"clickUrl":"https://ads.example/"}]},` +
		`{"display_type":"web-google","results":[` +
		`{"title":"<b>DeepSeek Harness</b> | Explore the limits",` +
		`"clickUrl":"https://www.deepseek.com/en/harness/?utm_source=sp&utm_medium=link",` +
		`"description":"Use <b>DeepSeek Harness</b> to work with documents &amp; more."},` +
		`{"title":"GitHub fallback","titleNoFormatting":"DeepSeek Harness on GitHub",` +
		`"clickUrl":"https://github.com/deepseek-ai/deepseek-harness",` +
		`"description":"17 hours ago <b>...</b> <b>DeepSeek Harness</b> ( dsh ) is an agent harness."}` +
		`]}]}}}}`

	results, err := parseStartpage(startpageSERPBody(payload), "startpage")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("got %d results, wanted 2: %+v", len(results), results)
	}

	first := results[0]
	if first.Title != "DeepSeek Harness | Explore the limits" {
		t.Errorf("title = %q", first.Title)
	}
	if first.Link != "https://www.deepseek.com/en/harness/" {
		t.Errorf("link = %q, wanted tracking parameters removed", first.Link)
	}
	if first.Description != "Use DeepSeek Harness to work with documents & more." {
		t.Errorf("description = %q", first.Description)
	}
	if len(first.Sources) != 1 || first.Sources[0] != "startpage" {
		t.Errorf("sources = %v", first.Sources)
	}

	second := results[1]
	if second.Title != "DeepSeek Harness on GitHub" {
		t.Errorf("title = %q, wanted titleNoFormatting", second.Title)
	}
	if strings.HasPrefix(second.Description, "17 hours ago") {
		t.Errorf("description still has a relative date: %q", second.Description)
	}
	if !strings.HasPrefix(second.Description, "DeepSeek Harness ( dsh )") {
		t.Errorf("description = %q", second.Description)
	}
}

func TestParseStartpageNoPayload(t *testing.T) {
	_, err := parseStartpage([]byte(`<html><body>blocked</body></html>`), "startpage")
	if !errors.Is(err, search.ErrCaptcha) {
		t.Fatalf("got %v, wanted ErrCaptcha", err)
	}
}

func TestParseStartpageMalformed(t *testing.T) {
	_, err := parseStartpage([]byte(startpageSERPStart+`"render":{`), "startpage")
	if err == nil {
		t.Fatal("expected an error for a truncated payload")
	}
	if errors.Is(err, search.ErrCaptcha) {
		t.Fatalf("truncated payload should not be reported as a captcha: %v", err)
	}
}

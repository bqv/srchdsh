package engines

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"testing"

	"golang.org/x/crypto/pbkdf2"

	"git.sr.ht/~cmcevoy/srchd/search"
)

// newAltchaChallenge builds a challenge whose answer is counter.
func newAltchaChallenge(counter, cost int) altchaChallenge {
	nonce := []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88,
		0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x00}
	salt := []byte{0x0f, 0x1e, 0x2d, 0x3c, 0x4b, 0x5a, 0x69, 0x78,
		0x87, 0x96, 0xa5, 0xb4, 0xc3, 0xd2, 0xe1, 0xf0}

	secret := make([]byte, len(nonce)+4)
	copy(secret, nonce)
	binary.BigEndian.PutUint32(secret[len(nonce):], uint32(counter))
	key := pbkdf2.Key(secret, salt, cost, 32, sha256.New)

	var c altchaChallenge
	c.Parameters.Algorithm = mojeekAltchaAlgorithm
	c.Parameters.Cost = cost
	c.Parameters.KeyLength = 32
	c.Parameters.Nonce = hex.EncodeToString(nonce)
	c.Parameters.Salt = hex.EncodeToString(salt)
	// Eight bytes is enough to make a wrong counter effectively impossible
	// without pretending to be the full key.
	c.Parameters.KeyPrefix = hex.EncodeToString(key[:8])
	return c
}

func TestSolveAltcha(t *testing.T) {
	const counter = 42

	c := newAltchaChallenge(counter, 200)

	gotCounter, gotKey, err := solveAltcha(context.Background(), c)
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	if gotCounter != counter {
		t.Fatalf("counter = %d, wanted %d", gotCounter, counter)
	}
	if len(gotKey) != 64 {
		t.Fatalf("key = %q, wanted 32 bytes of hex", gotKey)
	}
}

func TestSolveAltchaUnsupportedAlgorithm(t *testing.T) {
	c := newAltchaChallenge(1, 100)
	c.Parameters.Algorithm = "PBKDF2/SHA-512"

	if _, _, err := solveAltcha(context.Background(), c); !errors.Is(err, search.ErrCaptcha) {
		t.Fatalf("got %v, wanted ErrCaptcha", err)
	}
}

func TestSolveAltchaCancelled(t *testing.T) {
	c := newAltchaChallenge(500, 100000)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, _, err := solveAltcha(ctx, c); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, wanted context.Canceled", err)
	}
}

func TestParseMojeek(t *testing.T) {
	body := []byte(`<html><body><div class="results"><ul class="results-standard">` +
		`<li class="r1">` +
		`<a title="https://www.deepseek.com/en/harness/" href="https://www.deepseek.com/en/harness/?utm_source=mojeek&amp;utm_medium=link" class="ob">` +
		`<p class="i"><span class="url">https://www.deepseek.com</span></p></a>` +
		`<h2><a class="title" href="https://www.deepseek.com/en/harness/?utm_source=mojeek&amp;utm_medium=link">DeepSeek Harness</a></h2>` +
		`<p class="s">An agent harness with composable plugins.</p>` +
		`<p class="more"><a href="/search?q=site%3Adeepseek.com">See more</a></p></li>` +
		`<li class="r2">` +
		`<a href="https://example.org/ad" class="ob"><p class="i"></p></a></li>` +
		`<li class="r3">` +
		`<a title="https://github.com/deepseek-ai/deepseek-harness" href="https://github.com/deepseek-ai/deepseek-harness" class="ob"><p class="i"></p></a>` +
		`<h2><a class="title" href="https://github.com/deepseek-ai/deepseek-harness">Everything is a plugin</a></h2>` +
		`<p class="s">Source code and documentation.</p></li>` +
		`</ul></div></body></html>`)

	results, err := parseMojeek(body, "mojeek")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("got %d results, wanted 2: %+v", len(results), results)
	}

	first := results[0]
	if first.Title != "DeepSeek Harness" {
		t.Errorf("title = %q", first.Title)
	}
	if first.Link != "https://www.deepseek.com/en/harness/" {
		t.Errorf("link = %q, wanted tracking parameters removed", first.Link)
	}
	if first.Description != "An agent harness with composable plugins." {
		t.Errorf("description = %q", first.Description)
	}
	if len(first.Sources) != 1 || first.Sources[0] != "mojeek" {
		t.Errorf("sources = %v", first.Sources)
	}

	if results[1].Link != "https://github.com/deepseek-ai/deepseek-harness" {
		t.Errorf("second link = %q", results[1].Link)
	}
}

func TestParseMojeekChallengePage(t *testing.T) {
	body := []byte(`<html><body><div id="altcha"></div></body></html>`)

	if _, err := parseMojeek(body, "mojeek"); !errors.Is(err, search.ErrCaptcha) {
		t.Fatalf("got %v, wanted ErrCaptcha", err)
	}
}

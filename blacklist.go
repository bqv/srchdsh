package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"

	"srchdsh/search"
)

// Implements a basic blacklist.
type Blacklist struct {
	regexps []*regexp.Regexp
}

// Creates a new blacklist.
func newBlacklist() *Blacklist {
	// TODO: Regular expressions are probably not the best thing to use.
	return &Blacklist{
		regexps: []*regexp.Regexp{},
	}
}

// Adds a domain to the blacklist.
//
// This is a convenience wrapper over AddPattern, which itself is a convenience
// wrapper over AddRegexp.
func (b *Blacklist) AddDomain(dom string) error {
	return b.AddPattern(fmt.Sprintf("*://%s/*", dom))
}

// Adds a match pattern to the blacklist.
// Please see
// https://developer.mozilla.org/en-US/docs/Mozilla/Add-ons/WebExtensions/Match_patterns
// for documentation on match patterns.
//
// This is a convenience wrapper over AddRegexp.
func (b *Blacklist) AddPattern(rule string) error {
	exp := strings.Builder{}
	stage := 0

	exp.WriteRune('^')

	// TODO: This is extremely hacky, but it should work for valid
	// patterns.
	// This should be rewritten eventually to properly parse the rules as
	// they are specified in MDN.
	for i, r := range rule {
		switch stage {
		case 0: // scheme
			if r == ':' {
				// into hostname
				exp.WriteRune(':')
				stage++
			} else if r == '*' {
				exp.WriteString(`[^:]*`)
			} else {
				exp.WriteString(regexp.QuoteMeta(string(r)))
			}
		case 1:
			if rule[i-2] != ':' && rule[i-1] != ':' && rule[i-1] != '/' && r == '/' {
				// into path
				exp.WriteRune('/')
				stage++
			} else if r == '*' {
				exp.WriteString(`([^\.]*\.)*`)
			} else if rule[i-1] != '*' {
				exp.WriteString(regexp.QuoteMeta(string(r)))
			}
		case 2:
			if r == '*' {
				if strings.LastIndexByte(rule[i:], '/') > 0 {
					// There are more path components
					exp.WriteString(`[^/]*`)
				} else {
					// No more path components
					exp.WriteString(`.*`)
				}
			} else {
				exp.WriteString(regexp.QuoteMeta(string(r)))
			}
		}
	}

	exp.WriteRune('$')

	return b.AddRegexp(exp.String())
}

// Adds a regular expression to the blacklist.
func (b *Blacklist) AddRegexp(rule string) error {
	re, err := regexp.Compile(rule)
	if err != nil {
		return err
	}

	b.regexps = append(b.regexps, re)
	return nil
}

// Returns true if the link should be filtered by the blacklist.
func (b *Blacklist) Contains(link string) bool {
	link = normalizeLink(link)

	for _, re := range b.regexps {
		if re.MatchString(link) {
			return true
		}
	}

	return false
}

// Filters search results using a blacklist defined in the config.
//
// Returns the modified slice and the number of items that have been dropped.
// Do not use the original slice.
func (b *Blacklist) Filter(res []search.Result) (out []search.Result, dropped int) {
	out = res
	outi := 0

	for i := 0; i < len(res); i++ {
		result := res[i]

		if !b.Contains(result.Link) {
			// Do not drop
			out[outi] = result
			outi++
		} else {
			dropped++
		}
	}

	out = out[:outi]
	return
}

// Loads a uBlacklist ruleset from disk.
func (b *Blacklist) LoadFile(path string) (n int, err error) {
	h, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer h.Close()

	r := bufio.NewScanner(h)

	for r.Scan() {
		line := strings.TrimSpace(r.Text())
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}

		if strings.HasPrefix(line, "/") {
			re := strings.TrimPrefix(line, "/")
			re = strings.TrimSuffix(re, "/")
			if err := b.AddRegexp(re); err != nil {
				return n, err
			}
			n++
		} else {
			if err := b.AddPattern(line); err != nil {
				return n, err
			}
			n++
		}
	}

	return
}

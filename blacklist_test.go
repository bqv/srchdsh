package main

import (
	"reflect"
	"testing"

	"srchdsh/search"
)

func TestBlacklistAddDomain(t *testing.T) {
	b := newBlacklist()

	b.AddDomain("example.com")

	test := []string{
		"https://example.com",
		"http://example.com",
		"http://example.com/",
		"http://example.com/cbde",
	}

	itest := []string{
		"https://example.co",
	}

	for _, v := range test {
		if !b.Contains(v) {
			t.Errorf("rule does not match %q", v)
		}
	}

	for _, v := range itest {
		if b.Contains(v) {
			t.Errorf("rule matches %q when it shouldn't", v)
		}
	}
}

func TestBlacklistAddPattern(t *testing.T) {
	b := newBlacklist()

	b.AddPattern("*://example.com/*")

	test := []string{
		"https://example.com",
		"http://example.com",
		"http://example.com/",
		"http://example.com/cbde",
	}

	itest := []string{
		"https://example.co",
	}

	for _, v := range test {
		if !b.Contains(v) {
			t.Errorf("rule does not match %q", v)
		}
	}

	for _, v := range itest {
		if b.Contains(v) {
			t.Errorf("rule matches %q when it shouldn't", v)
		}
	}
}

func TestBlacklistAddPattern_Hostname(t *testing.T) {
	b := newBlacklist()

	b.AddPattern("*://*.example.com/*")

	test := []string{
		"http://example.com",
		"http://www.example.com",
		"http://aaaaaa.www.example.com",
	}

	itest := []string{
		"https://example.co",
		"http://wwwexample.com",
	}

	for _, v := range test {
		if !b.Contains(v) {
			t.Errorf("rule does not match %q", v)
		}
	}

	for _, v := range itest {
		if b.Contains(v) {
			t.Errorf("rule matches %q when it shouldn't", v)
		}
	}
}

func TestBlacklistAddPattern_NonRootPath(t *testing.T) {
	b := newBlacklist()

	b.AddPattern("*://example.com/abc/*")

	test := []string{
		"https://example.com/abc/",
		"https://example.com/abc/def",
	}

	itest := []string{
		"https://example.com/",
		"https://example.com/def",
	}

	for _, v := range test {
		if !b.Contains(v) {
			t.Errorf("rule does not match %q", v)
		}
	}

	for _, v := range itest {
		if b.Contains(v) {
			t.Errorf("rule matches %q when it shouldn't", v)
		}
	}
}

func TestBlacklistAddPattern_PathComponentWildcards(t *testing.T) {
	b := newBlacklist()

	b.AddPattern("*://example.com/*/b/*")

	test := []string{
		"https://example.com/a/b/c",
		"https://example.com/a/b/c/",
		"https://example.com/d/b/f",
		"https://example.com/d/b/f/",
	}

	itest := []string{
		"https://example.com/",
		"https://example.com/def",
		"https://example.com/c/a/b",
		"https://example.com/c/a/b/",
	}

	for _, v := range test {
		if !b.Contains(v) {
			t.Errorf("rule does not match %q", v)
		}
	}

	for _, v := range itest {
		if b.Contains(v) {
			t.Errorf("rule matches %q when it shouldn't", v)
		}
	}
}

func TestBlacklistFilter(t *testing.T) {
	b := newBlacklist()

	b.AddPattern("*://example.com/*")

	res := []search.Result{
		{Link: "https://example.com"},
		{Link: "https://example.com/abc"},
		{Link: "https://coolwebsite.com/"},
		{Link: "https://example.com/abc/def"},
		{Link: "https://coolwebsite.com/abc"},
	}

	exp := []search.Result{
		{Link: "https://coolwebsite.com/"},
		{Link: "https://coolwebsite.com/abc"},
	}

	act, dropped := b.Filter(res)

	if dropped != 3 {
		t.Errorf("expected dropped = %d, got %d", 3, dropped)
	}

	if !reflect.DeepEqual(exp, act) {
		t.Errorf("expected %+v, got %+v", exp, act)
	}
}

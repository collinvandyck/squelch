package main

import (
	"slices"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestFindURLs checks that URLs are found in a line and stripped of the punctuation around them.
func TestFindURLs(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"none", "nothing to see here", nil},
		{"bare", "https://example.com", []string{"https://example.com"}},
		{"http", "visit http://example.com/x now", []string{"http://example.com/x"}},
		{"path and query", "open https://a.io/p/q?x=1&y=2#frag", []string{"https://a.io/p/q?x=1&y=2#frag"}},
		{"trailing period", "see https://example.com/a.", []string{"https://example.com/a"}},
		{"trailing comma", "https://example.com/a, then", []string{"https://example.com/a"}},
		{"in parens", "(see https://example.com/a).", []string{"https://example.com/a"}},
		{"balanced parens kept", "https://en.wikipedia.org/wiki/Go_(game)", []string{"https://en.wikipedia.org/wiki/Go_(game)"}},
		{"in brackets", "[https://example.com/a]", []string{"https://example.com/a"}},
		{"quoted", `url="https://example.com/a"`, []string{"https://example.com/a"}},
		{"angle brackets", "<https://example.com/a>", []string{"https://example.com/a"}},
		{"several", "https://a.io and https://b.io", []string{"https://a.io", "https://b.io"}},
		{"other scheme", "ftp://example.com", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := findURLs(tt.in); !slices.Equal(got, tt.want) {
				t.Errorf("findURLs(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestLinkListAdd checks that links keep the order they were first seen in, ignore repeats, and see through the
// command's own escape codes.
func TestLinkListAdd(t *testing.T) {
	var l linkList
	l.add([]string{"go to https://b.io/login", "\x1b[31mhttps://a.io:8443/x\x1b[0m"})
	l.add([]string{"again https://b.io/login", "https://", "https://c.io"})
	want := []link{
		{url: "https://b.io/login", host: "b.io"},
		{url: "https://a.io:8443/x", host: "a.io"},
		{url: "https://c.io", host: "c.io"},
	}
	if !slices.Equal(l.links, want) {
		t.Errorf("links = %+v, want %+v", l.links, want)
	}
}

// TestLinkListView checks that each link renders as the globe and its host, hyperlinked to the full URL.
func TestLinkListView(t *testing.T) {
	var l linkList
	l.add([]string{"https://console.example.org/projects/foo", "https://example.com/a"})
	want := "🌍 " + ansi.SetHyperlink("https://console.example.org/projects/foo") + "console.example.org" +
		ansi.ResetHyperlink() + "\n" +
		"🌍 " + ansi.SetHyperlink("https://example.com/a") + "example.com" + ansi.ResetHyperlink()
	if got := l.view(0); got != want {
		t.Errorf("view =\n%q\nwant\n%q", got, want)
	}
}

// TestLinkListViewTruncates checks that a host too wide for the terminal is cut to fit.
func TestLinkListViewTruncates(t *testing.T) {
	var l linkList
	l.add([]string{"https://console.example.org"})
	if got, want := ansi.StringWidth(l.view(10)), 10; got != want {
		t.Errorf("view width = %d, want %d", got, want)
	}
}

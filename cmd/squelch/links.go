package main

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

type (
	// linkList collects the distinct URLs found in the command's output, in the order they first appeared.
	linkList struct {
		links []link
		seen  map[string]bool
	}
	link struct {
		url  string
		host string
	}
)

var urlPattern = regexp.MustCompile("https?://[^\\s<>\"'`]+")

func (l *linkList) add(lines []string) {
	for _, line := range lines {
		for _, raw := range findURLs(ansi.Strip(line)) {
			if l.seen[raw] {
				continue
			}
			u, err := url.Parse(raw)
			if err != nil || u.Hostname() == "" {
				continue
			}
			if l.seen == nil {
				l.seen = make(map[string]bool)
			}
			l.seen[raw] = true
			l.links = append(l.links, link{url: raw, host: u.Hostname()})
		}
	}
}

// view renders one line per link, showing the host as an OSC 8 hyperlink to the full URL.
func (l linkList) view(width int) string {
	const prefix = "🌍 "
	lines := make([]string, len(l.links))
	for i, lk := range l.links {
		text := lk.host
		if width > 0 {
			text = ansi.Truncate(text, width-ansi.StringWidth(prefix), "…")
		}
		lines[i] = prefix + ansi.SetHyperlink(lk.url) + text + ansi.ResetHyperlink()
	}
	return strings.Join(lines, "\n")
}

// findURLs returns the http and https URLs in s, without the punctuation that tends to follow a URL in prose.
func findURLs(s string) []string {
	urls := urlPattern.FindAllString(s, -1)
	for i, u := range urls {
		urls[i] = trimURL(u)
	}
	return urls
}

// trimURL drops trailing sentence punctuation, and closing brackets that have no opener inside the URL, so that
// "(see https://example.com/a)." yields https://example.com/a.
func trimURL(u string) string {
	for len(u) > 0 {
		last := u[len(u)-1]
		switch last {
		case '.', ',', ';', ':', '!', '?':
		case ')':
			if strings.Count(u, "(") >= strings.Count(u, ")") {
				return u
			}
		case ']':
			if strings.Count(u, "[") >= strings.Count(u, "]") {
				return u
			}
		default:
			return u
		}
		u = u[:len(u)-1]
	}
	return u
}

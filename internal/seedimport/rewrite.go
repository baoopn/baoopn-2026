package seedimport

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/baoopn/baoopn-2026/internal/markdown"
)

var (
	partHeading = regexp.MustCompile(`^#{1,2}\s*Part\s+\d+:`)
	mdImage     = regexp.MustCompile(`!\[[^\]]*\]\((/[^)\s]+)(?:\s+"[^"]*")?\)`)
	htmlImage   = regexp.MustCompile(`<img[^>]+src="(/[^"]+)"`)
)

// stripPartHeading drops a leading "## Part N: …" line (the title is shown
// separately) and the blank lines after it.
func stripPartHeading(md string) string {
	md = strings.TrimLeft(strings.ReplaceAll(md, "\r\n", "\n"), string(rune(0xFEFF)))
	first, rest, _ := strings.Cut(md, "\n")
	if !partHeading.MatchString(strings.TrimSpace(first)) {
		return md
	}
	return strings.TrimLeft(rest, "\n")
}

// rewriteImages replaces local image paths (Markdown images and <img src>)
// with what resolve returns. Paths resolve cannot map are left unchanged.
func rewriteImages(md string, resolve func(path string) (string, bool)) string {
	for _, re := range []*regexp.Regexp{mdImage, htmlImage} {
		md = replaceGroup(md, re, resolve)
	}
	return md
}

// replaceGroup rewrites submatch 1 of every match of re.
func replaceGroup(s string, re *regexp.Regexp, resolve func(string) (string, bool)) string {
	idx := re.FindAllStringSubmatchIndex(s, -1)
	if idx == nil {
		return s
	}
	var b strings.Builder
	last := 0
	for _, m := range idx {
		start, end := m[2], m[3]
		b.WriteString(s[last:start])
		if to, ok := resolve(s[start:end]); ok {
			b.WriteString(to)
		} else {
			b.WriteString(s[start:end])
		}
		last = end
	}
	b.WriteString(s[last:])
	return b.String()
}

// localPaths lists the distinct local image paths in md, in order.
func localPaths(md string) []string {
	var out []string
	seen := map[string]bool{}
	for _, re := range []*regexp.Regexp{mdImage, htmlImage} {
		for _, m := range re.FindAllStringSubmatch(md, -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				out = append(out, m[1])
			}
		}
	}
	return out
}

// postSlug is the slug of page in series.
func postSlug(series, page string) string { return markdown.Slugify(series + "-" + page) }

// legacyPath is the v3 URL of page no in series.
func legacyPath(series string, no int) string { return "/blog/" + series + "/" + strconv.Itoa(no) }

// publishedAt is the series date at 12:00 UTC plus no minutes, so the pages
// of a series sort in order.
func publishedAt(date string, no int) (time.Time, error) {
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		return time.Time{}, err
	}
	return d.Add(12*time.Hour + time.Duration(no)*time.Minute).UTC(), nil
}

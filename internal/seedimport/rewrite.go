package seedimport

import (
	"regexp"
	"strings"
)

// screenshotRef is ![alt](screenshot:<id>) with an optional "title".
var screenshotRef = regexp.MustCompile(`!\[[^\]]*\]\((screenshot:([A-Za-z0-9_-]+))(?:\s+"[^"]*")?\)`)

// rewriteScreenshots replaces every screenshot:<id> image target with the
// URL resolve returns. An image resolve cannot map is removed, and a line
// left blank by that is dropped.
func rewriteScreenshots(md string, resolve func(id string) (string, bool)) string {
	md = strings.TrimLeft(strings.ReplaceAll(md, "\r\n", "\n"), string(rune(0xFEFF)))
	lines := strings.Split(md, "\n")
	out := lines[:0]
	for _, line := range lines {
		idx := screenshotRef.FindAllStringSubmatchIndex(line, -1)
		if idx == nil {
			out = append(out, line)
			continue
		}
		var b strings.Builder
		last := 0
		for _, m := range idx {
			if to, ok := resolve(line[m[4]:m[5]]); ok {
				b.WriteString(line[last:m[2]])
				b.WriteString(to)
				last = m[3]
			} else {
				b.WriteString(line[last:m[0]])
				last = m[1]
			}
		}
		b.WriteString(line[last:])
		if s := b.String(); strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, "\n")
}

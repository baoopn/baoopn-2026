package mail

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// fetchCap is the most a received email can be (the 2 MB fetch cap).
const fetchCap = 2 << 20

// fill repeats unit up to about fetchCap bytes.
func fill(unit string) string { return strings.Repeat(unit, fetchCap/len(unit)) }

// within fails the test when run takes longer than limit. The limit is generous (the
// cutter takes milliseconds here; before it was linear the first case took most of
// an hour), so a slow or loaded machine does not make it flaky.
func within(t *testing.T, limit time.Duration, run func()) {
	t.Helper()
	start := time.Now()
	done := make(chan struct{})
	go func() { defer close(done); run() }()
	select {
	case <-done:
		t.Logf("took %v", time.Since(start))
	case <-time.After(limit):
		t.Fatalf("still running after %v: the cutter is not linear", limit)
	}
}

// Header-shaped lines, dividers and attributions, as many as fit in the largest email,
// are each read a bounded number of times. The cutter is run on all of it (no cap) so
// that its own cost is what is measured.
func TestCutterIsLinear(t *testing.T) {
	ours := linesOf(weSent)
	for name, unit := range map[string]string{
		"from lines":                       "From: a@b.co\n",
		"from lines that name us":          "From: Bao Nguyen\n",
		"header blocks with no date":       "From: a@b.co\nTo: c@d.co\nSubject: x\n",
		"dated blocks over our lines":      "From: a@b.co\nSent: 2026\nTo: x\n\nWe have August 14 open.\n",
		"original message dividers":        "-----Original Message-----\n\n\n",
		"original message over headers":    "-----Original Message-----\nFrom: a@b.co\nTo: c\n",
		"attributions":                     "On 2026 a@b.co wrote:\n\n\n",
		"wrapped attributions, never done": "On 2026 a@b.co\nx\ny\n",
	} {
		t.Run(name, func(t *testing.T) {
			lines := strings.Split(fill(unit), "\n")
			within(t, 20*time.Second, func() { newCutter(lines, product, ours).cut() })
		})
	}
}

// The whole of the largest email, as text and as HTML, is cleaned quickly.
func TestCleanBodyOfTheLargestEmail(t *testing.T) {
	for name, in := range map[string]Received{
		"text":               {Text: fill("From: a@b.co\n")},
		"html":               {HTML: fill("From: a@b.co<br>")},
		"html in quotations": {HTML: "<p>Yes!</p>" + fill("<blockquote>From: a@b.co<br>")},
	} {
		t.Run(name, func(t *testing.T) {
			in.Sender, in.Sent = product, weSent
			within(t, 20*time.Second, func() {
				body, full, ok := CleanBody(in)
				if !ok || body == "" || utf8.RuneCountInString(body) > maxInboundRunes || utf8.RuneCountInString(full) > maxFullRunes {
					t.Errorf("body %d characters, full %d (ok %v)", utf8.RuneCountInString(body), utf8.RuneCountInString(full), ok)
				}
			})
		})
	}
}

// Text past maxCutBytes is dropped at a line end, so history that runs on past it is
// still recognised as all ours.
func TestCutterCapEndsOnALine(t *testing.T) {
	history := strings.Repeat("We have August 14 open.\nShall we talk?\n", maxCutBytes/20)
	in := "Thank you, booked.\n\n" + outlookHead + "\n\n" + history
	body, full, ok := CleanBody(Received{Text: in, Sender: product, Sent: weSent})
	if !ok || body != "Thank you, booked." {
		t.Fatalf("body = %.80q (ok %v)", body, ok)
	}
	if utf8.RuneCountInString(full) != maxFullRunes {
		t.Fatalf("the whole email is kept, capped as before: %d characters", utf8.RuneCountInString(full))
	}
}

func TestHead(t *testing.T) {
	for _, tc := range []struct {
		in    string
		limit int
		want  string
	}{
		{"short", 10, "short"},
		{"one\ntwo\nthree", 10, "one\ntwo"},
		{"one\ntwo\n", 8, "one\ntwo\n"},
		{"abcdef", 4, "abcd"},
		// "é" is two bytes: a cut inside it backs off to before it.
		{"aaé", 3, "aa"},
		{"\n" + strings.Repeat("x", 9), 5, ""},
	} {
		if got := head(tc.in, tc.limit); got != tc.want {
			t.Errorf("head(%q, %d) = %q, want %q", tc.in, tc.limit, got, tc.want)
		}
	}
}

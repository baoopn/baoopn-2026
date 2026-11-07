package mail

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// product is our sender name as the tests' attribution lines carry it.
const product = "Bao Nguyen"

const (
	gmailLine = "On Mon, Oct 5, 2026 at 3:14 PM Bao Nguyen <bao@mail.baoopn.com> wrote:"
	// French puts a no-break space (U+00A0) before the colon.
	frenchLine  = "Le lun. 5 oct. 2026 \xc3\xa0 15:14, Bao Nguyen <bao@mail.baoopn.com> a \xc3\xa9crit" + string(rune(0xa0)) + ":"
	outlookHead = "From: Bao Nguyen <bao@mail.baoopn.com>\nSent: Monday, October 5, 2026 3:14 PM\nTo: Priya Shah <priya@example.com>\nSubject: Re: Your message"
	footerLine  = "https://baoopn.com"
)

// weSent is what we mailed on the tests' thread before the visitor answered.
var weSent = []string{
	replyText("We have August 14 open.\nShall we talk?", footerLine),
	replyText("Do you want the big room?\nAnd catering?", footerLine),
}

// clean is CleanBody for an email on that thread.
func clean(text, htmlPart string, attachments int) (string, bool) {
	body, _, ok := CleanBody(Received{Text: text, HTML: htmlPart, Attachments: attachments, Sender: product, Sent: weSent})
	return body, ok
}

func cleaned(text string) (string, bool) { return clean(text, "", 0) }

type cutCase struct{ in, want string }

func checkCut(t *testing.T, cases map[string]cutCase) {
	t.Helper()
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := cleaned(tc.in)
			if !ok || got != tc.want {
				t.Fatalf("got  %q (ok %v)\nwant %q", got, ok, tc.want)
			}
		})
	}
}

// What each mail app puts under a reply written on top is removed.
func TestCleanBodyRemovesHistoryUnderAReply(t *testing.T) {
	checkCut(t, map[string]cutCase{
		"gmail": {
			"Yes, the 14th works for us!\n\n" + gmailLine + "\n> We have August 14 open.\n> Shall we talk?\n",
			"Yes, the 14th works for us!",
		},
		"gmail, attribution wrapped over two lines": {
			"Sounds good.\n\nOn Mon, Oct 5, 2026 at 3:14 PM Bao Nguyen <\nbao@mail.baoopn.com> wrote:\n\n> We have August 14 open.\n",
			"Sounds good.",
		},
		"gmail, attribution with only our name and a date": {
			"Sounds good.\n\nOn Oct 5, 2026, Bao Nguyen wrote:\n> We have August 14 open.\n",
			"Sounds good.",
		},
		"apple mail quotes its attribution too": {
			"Yes!\n\nSent from my iPhone\n\n> On Oct 5, 2026, at 3:14 PM, Bao Nguyen <bao@mail.baoopn.com> wrote:\n> \n> We have August 14 open.\n> Shall we talk?\n",
			"Yes!\n\nSent from my iPhone",
		},
		"outlook desktop": {
			"Thank you, booked.\r\n\r\n" + strings.ReplaceAll(outlookHead, "\n", "\r\n") + "\r\n\r\nWe have August 14 open.\r\nShall we talk?",
			"Thank you, booked.",
		},
		"outlook desktop, with our footer and uneven spaces": {
			"Thank you, booked.\n\n" + outlookHead + "\n\nWe  have August 14   open.\n  Shall we talk?\n\n--\n" + footerLine + "\n",
			"Thank you, booked.",
		},
		"outlook desktop, original message": {
			"Thank you, booked.\r\n\r\n-----Original Message-----\r\nFrom: Northlight Hall\r\nSent: Monday\r\nSubject: Re: Your inquiry\r\n\r\nWe have August 14 open.",
			"Thank you, booked.",
		},
		"outlook web, under a divider": {
			"Great, thanks.\n\n________________________________\n" + outlookHead + "\n\nWe have August 14 open.",
			"Great, thanks.",
		},
		"french": {
			"Oui, le 14 nous convient.\n\n" + frenchLine + "\n\n> We have August 14 open.\n> Shall we talk?\n",
			"Oui, le 14 nous convient.",
		},
		"spanish": {
			"S\xc3\xad, el 14 nos va bien.\n\nEl lun, 5 oct 2026 a las 15:14, Bao Nguyen (<bao@mail.baoopn.com>) escribi\xc3\xb3:\n> We have August 14 open.\n",
			"S\xc3\xad, el 14 nos va bien.",
		},
		"german": {
			"Ja, der 14. passt.\n\nAm Mo., 5. Okt. 2026 um 15:14 Uhr schrieb Bao Nguyen <bao@mail.baoopn.com>:\n> We have August 14 open.\n",
			"Ja, der 14. passt.",
		},
	})
}

// A visitor's words are kept wherever they are: under the quoted text, between its
// lines, and when they only look like what a mail app writes. Every reply the cutter
// once lost is here.
func TestCleanBodyKeepsTheCouplesWords(t *testing.T) {
	same := func(s string) cutCase { return cutCase{s, s} }
	checkCut(t, map[string]cutCase{
		"a reply under the quoted text": {
			gmailLine + "\n> Can you do the 14th?\n\nYes, the 14th works for us. Please send the contract.",
			"Yes, the 14th works for us. Please send the contract.",
		},
		"answers between the quoted lines": {
			"Hi!\n\n" + gmailLine + "\n> Do you want the big room?\nYes please.\n> And catering?\nNo thanks.",
			"Hi!\n\nYes please.\nNo thanks.",
		},
		"a sentence that ends in wrote:":                                  same("Hi,\nOn Saturday we visited and my mother wrote:\nplease book the big room for 120 guests.\nThanks"),
		"wrote: a few lines after a line that starts with On":             same("On the 14th we need parking.\nAlso the DJ\nasked what the planner wrote:\nkeep it under $5,000."),
		"a dated sentence that ends in wrote: with their own words after": same("On Oct 5, 2026 my planner wrote:\nplease keep the big room.\nIs that still fine?"),
		"greater-than signs that are not quotes":                          same("Our budget:\n> 100 guests\n>= $5000\nthanks"),
		"a timeline that reads like mail headers":                         same("Timeline\nFrom: 2pm ceremony\nTo: 6pm\nSubject: to change\nplease confirm"),
		"one quoted line with nothing introducing it":                     same("> Do you need parking?\nYes, for about forty cars.\n> And a rain plan?\nYes please."),
		"a line that only starts like an attribution":                     same("On Saturday we visit the hall.\nSee you then."),
		"an attribution with nothing quoted under it, then text":          same("Thanks!\n\n" + gmailLine + "\n\nWe have August 14 open."),
		"nothing but quoted text is kept as it came":                      same(gmailLine + "\n> We have August 14 open.\n> Shall we talk?"),
		"only an outlook block is kept as it came":                        same(outlookHead + "\n\nWe have August 14 open."),

		// Outlook does not mark what it quotes, so answers typed inside it look like it.
		"outlook: answers typed inside the quoted message": same(
			"See my answers below in caps.\n\n" + outlookHead + "\n\nDo you want the big room?\nYES PLEASE AND PARKING FOR 40"),
		"outlook: answers typed under original message": same(
			"See my answers below in caps.\n\n-----Original Message-----\n" + outlookHead + "\n\nDo you want the big room?\nYES PLEASE AND PARKING FOR 40"),
		"a forwarded quote with a note on top": same(
			"Can you match the quote below?\n\nBegin forwarded message:\n\nFrom: Other Venue <sales@other.example>\nSubject: Your quote\nDate: October 1, 2026\nTo: Priya <priya@example.com>\n\nThe big room is $4,000 all in for 120 guests."),
		"a forwarded message under a gmail divider": same(
			"See below.\n\n---------- Forwarded message ---------\nFrom: Someone <s@example.com>\nDate: Mon, Oct 5, 2026\nSubject: Hall\n\nText of it."),

		// Their own lines that read like mail headers.
		"a plan written as from, to and date": same(
			"Here is our plan\nFrom: 2:00 pm ceremony\nTo: 11:00 pm\nDate: June 5, 2027\nplease confirm you can do it\nthanks Priya"),
		"an introduction written as headers": same(
			"From: Priya and Sam\nDate: 12/06/2027\nSubject: our wedding\nWe would like the big room for 120 guests."),

		"an introduction written as headers, after a greeting": same(
			"Hello!\nFrom: Priya and Sam\nDate: 12/06/2027\nSubject: our wedding\nWe would like the big room for 120 guests."),
		"a plan written as headers, with nothing after it": same(
			"Our day:\nFrom: 2:00 pm ceremony\nTo: 11:00 pm\nDate: June 5, 2027"),
		"a plan written as headers, then a line of ours": same(
			"Our day:\nFrom: 2:00 pm ceremony\nTo: 11:00 pm\nDate: June 5, 2027\nShall we talk?"),

		// An attribution takes nothing above it with it.
		"a question that starts with Am, right above the attribution": {
			"Thanks so much!\nAm I able to pay the deposit by e-transfer?\n" + gmailLine + "\n> We have August 14 open.\n> Shall we talk?",
			"Thanks so much!\nAm I able to pay the deposit by e-transfer?",
		},
		"a french sentence that starts with Le, right above the attribution": {
			"Bonjour,\nLe 5 juin nous convient parfaitement.\n" + frenchLine + "\n> We have August 14 open.",
			"Bonjour,\nLe 5 juin nous convient parfaitement.",
		},
		"a dated sentence that starts with On, right above the attribution": {
			"On June 5, 2027 we are free all day.\n" + gmailLine + "\n> We have August 14 open.",
			"On June 5, 2027 we are free all day.",
		},
		"a sentence that starts with On and, lines later, one that names an address and ends in wrote:": same(
			"Hi,\nOn the 14th we need parking.\nPlease tell bao@mail.baoopn.com what the planner wrote:\n> keep it under $5,000"),
		"an attribution with nothing at all under it": same("Thanks!\n\n" + gmailLine),
		"a last line that is a sentence":              same("We are still deciding.\nOn Oct 5, 2026 the other venue wrote:"),
		"a dated sentence, then their own > line":     same("On the phone on 10/05/2026 you wrote:\n> $4000 all in"),
		"a dated sentence and their own > line, between other words": same(
			"Hello,\nOn the phone on 10/05/2026 you wrote:\n> $4000 all in\nIs that still right?"),
		"their own > lines, introduced by nothing": same("Questions:\n> parking?\n> rain plan?\n> price for 120?"),
		"two > lines that are ours, introduced by nothing": same(
			"Sure.\n> We have August 14 open.\n> Shall we talk?"),
	})
}

// Where the cutter cannot be sure it keeps the history too. That is the acceptable
// mistake; these pin it, so a change that starts cutting here is a decision.
func TestCleanBodyKeepsHistoryItCannotBeSureOf(t *testing.T) {
	same := func(s string) cutCase { return cutCase{s, s} }
	checkCut(t, map[string]cutCase{
		"yahoo: unmarked history under an attribution": same(
			"Sounds good.\n\nOn Monday, October 5, 2026 at 03:14:22 p.m. MDT, Bao Nguyen <bao@mail.baoopn.com> wrote:\n\nWe have August 14 open.\nShall we talk?"),
		"gmail in vietnamese": same(
			"Vâng, ngày 14 được ạ.\n\nVào Th 2, 5 thg 10, 2026 vào lúc 15:14 Bao Nguyen <bao@mail.baoopn.com> đã viết:\n> We have August 14 open.\n> Shall we talk?"),
		"an attribution wrapped over five lines": same(
			"Sounds good.\n\nOn Mon, Oct 5, 2026\nat 3:14 PM\nNorthlight Hall\nvia Bao Nguyen\n<bao@mail.baoopn.com> wrote:\n> We have August 14 open."),
		"outlook headers with blank lines between them": same(
			"Thank you.\n\nFrom: Bao Nguyen <bao@mail.baoopn.com>\n\nSent: Monday, October 5, 2026 3:14 PM\n\nTo: Priya Shah <priya@example.com>\n\nSubject: Re: Your message\n\nWe have August 14 open."),
		"outlook history that is not what we sent": same(
			"Thank you.\n\n" + outlookHead + "\n\nWe have August 15 open."),
		"a quoted line the mail app wrapped lazily": {
			"Yes!\n\n" + gmailLine + "\n> We have August 14 open and would love to show you\nthe big room.\n> Shall we talk?",
			"Yes!\n\nthe big room.",
		},
	})
}

// HTML marks a quotation as a block. In mail with no text part the block goes when the
// reply stands outside it, and stays when the reply was written inside it.
func TestCleanBodyHTMLOnlyMail(t *testing.T) {
	attribution := `<div class="gmail_quote"><div class="gmail_attr">` + strings.ReplaceAll(strings.ReplaceAll(gmailLine, "<", "&lt;"), ">", "&gt;") + `<br></div>`
	quote := `<blockquote class="gmail_quote"><div>We have August 14 open.</div><blockquote>Is August 14 open?</blockquote><div>Shall we talk?</div></blockquote>`
	for name, tc := range map[string]struct{ html, want string }{
		"a reply above the quotation": {
			`<div dir="ltr">Yes, the 14th works for us!</div><br>` + attribution + quote + `</div>`,
			"Yes, the 14th works for us!",
		},
		"a quotation nothing introduces, and words after it": {
			`<div>Yes!</div>` + quote + `<div>See you then.</div>`,
			"Yes!\n\nSee you then.",
		},
		"a quotation left open": {
			`<p>Yes!</p><blockquote><p>We have August 14 open.`,
			"Yes!",
		},
		"everything inside the quotation is kept": {
			`<blockquote><div>We have August 14 open.</div><div>YES PLEASE</div></blockquote>`,
			"We have August 14 open.\nYES PLEASE",
		},
	} {
		t.Run(name, func(t *testing.T) {
			body, full, ok := CleanBody(Received{HTML: tc.html, Sender: product, Sent: weSent})
			if !ok || body != tc.want {
				t.Fatalf("got  %q (ok %v)\nwant %q", body, ok, tc.want)
			}
			if strings.Contains(tc.html, "YES PLEASE") {
				if full != "" {
					t.Fatalf("nothing was left out, so there is no whole email to keep: %q", full)
				}
				return
			}
			if !strings.Contains(full, "We have August 14 open.") || strings.Contains(full, "<blockquote") || strings.Contains(full, "<div") {
				t.Fatalf("the whole email is kept beside it, as text: %q", full)
			}
		})
	}
}

// Whatever the cutter takes out is kept beside the message, so a wrong cut loses
// nothing; when it takes nothing out there is no second copy.
func TestTheWholeEmailIsKeptWhenHistoryIsCut(t *testing.T) {
	whole := "Yes, the 14th works for us!\r\n\r\n\r\n\r\n" + gmailLine + "\r\n> We have August 14 open.\r\n> Shall we talk?\r\n"
	body, full, ok := CleanBody(Received{Text: whole, Sender: product, Sent: weSent})
	if !ok || body != "Yes, the 14th works for us!" {
		t.Fatalf("body = %q (ok %v)", body, ok)
	}
	if want := "Yes, the 14th works for us!\n\n" + gmailLine + "\n> We have August 14 open.\n> Shall we talk?"; full != want {
		t.Fatalf("full = %q\nwant   %q", full, want)
	}

	for name, in := range map[string]string{
		"no history":              "Yes, the 14th works for us!",
		"history that was kept":   "See my answers below in caps.\n\n" + outlookHead + "\n\nDo you want the big room?\nYES PLEASE",
		"nothing but a quotation": gmailLine + "\n> We have August 14 open.",
	} {
		if body, full, _ := CleanBody(Received{Text: in, Sender: product, Sent: weSent}); full != "" || body != in {
			t.Errorf("%s: nothing was left out, so nothing more is kept: body %q, full %q", name, body, full)
		}
	}

	// Both are capped, the whole email further out than the message.
	long := strings.Repeat("a", 30000) + "\n\n" + gmailLine + "\n> We have August 14 open."
	body, full, _ = CleanBody(Received{Text: long, Sender: product, Sent: weSent})
	if utf8.RuneCountInString(body) != maxInboundRunes || utf8.RuneCountInString(full) != maxFullRunes {
		t.Fatalf("caps: body %d, full %d", utf8.RuneCountInString(body), utf8.RuneCountInString(full))
	}
	// A long email with no history: what the cap left out of the message is kept too.
	body, full, _ = CleanBody(Received{Text: strings.Repeat("b", 9000), Sender: product})
	if utf8.RuneCountInString(body) != maxInboundRunes || utf8.RuneCountInString(full) != 9000 {
		t.Fatalf("a long email: body %d, full %d", utf8.RuneCountInString(body), utf8.RuneCountInString(full))
	}
	// No words and an attachment: a placeholder, and no whole email.
	if body, full, ok := CleanBody(Received{Attachments: 1}); !ok || body != noText || full != "" {
		t.Fatalf("no words: %q, %q, %v", body, full, ok)
	}
}

// Unmarked history is only ever taken out when all of it is what we sent: without the
// thread's own messages to compare with, it stays.
func TestUnmarkedHistoryNeedsWhatWeSent(t *testing.T) {
	in := "Thank you, booked.\n\n" + outlookHead + "\n\nWe have August 14 open.\nShall we talk?"
	if body, _, _ := CleanBody(Received{Text: in, Sender: product}); body != in {
		t.Fatalf("with nothing to compare with the history is kept: %q", body)
	}
	if body, _, _ := CleanBody(Received{Text: in, Sender: product, Sent: weSent}); body != "Thank you, booked." {
		t.Fatalf("with what we sent it is recognised: %q", body)
	}
}

// replyText is a reply as we mail it: the words, then the signature.
func replyText(body, footer string) string { return body + sigSep + footer + "\n" }

const sigSep = "\n\n-- \n"

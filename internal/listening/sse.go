package listening

import (
	"bufio"
	"bytes"
	"io"
	"strconv"
	"strings"
	"time"
)

// sseEvent is one dispatched Server-Sent Event.
type sseEvent struct {
	Type  string // "message" when no event: field was sent
	Data  string
	ID    string
	Retry time.Duration // > 0 when a valid retry: field preceded the dispatch
}

// sseReader parses a text/event-stream per the WHATWG spec: CR, LF and CRLF
// line endings, multi-line data, comments, id and retry fields.
type sseReader struct {
	sc *bufio.Scanner
	// onLine is called for every line read (comments and blank lines too), so
	// a read-idle watchdog can tell a live stream from a dead one.
	onLine func()

	typ, id string
	data    strings.Builder
	hasData bool
	retry   time.Duration
	started bool
	tooBig  bool // data of the current event exceeded maxSSEEvent
}

const (
	maxSSELine  = 1 << 20 // one line
	maxSSEEvent = 1 << 20 // all data: lines of one event
)

func newSSEReader(r io.Reader, onLine func()) *sseReader {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 4096), maxSSELine)
	sc.Split(scanSSELines)
	if onLine == nil {
		onLine = func() {}
	}
	return &sseReader{sc: sc, onLine: onLine}
}

// scanSSELines splits on CRLF, LF or a lone CR.
func scanSSELines(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		if data[i] == '\n' {
			return i + 1, data[:i], nil
		}
		// CR: swallow a following LF; wait for more data if CR is last.
		if i+1 < len(data) {
			if data[i+1] == '\n' {
				return i + 2, data[:i], nil
			}
			return i + 1, data[:i], nil
		}
		if !atEOF {
			return 0, nil, nil
		}
		return i + 1, data[:i], nil
	}
	if atEOF {
		// An unterminated final line is discarded by the spec (no dispatch).
		return len(data), nil, nil
	}
	return 0, nil, nil
}

// Next returns the next dispatched event, or the read error (io.EOF when the
// stream ended cleanly). A partially received event at EOF is dropped.
func (p *sseReader) Next() (sseEvent, error) {
	for p.sc.Scan() {
		p.onLine()
		line := p.sc.Text()
		if !p.started {
			p.started = true
			line = strings.TrimPrefix(line, "\xef\xbb\xbf") // UTF-8 BOM
		}
		if line == "" {
			if ev, ok := p.dispatch(); ok {
				return ev, nil
			}
			continue
		}
		if line[0] == ':' {
			continue // comment, e.g. ": keep-alive"
		}
		field, value, found := strings.Cut(line, ":")
		if found {
			value = strings.TrimPrefix(value, " ")
		}
		switch field {
		case "event":
			p.typ = value
		case "data":
			if p.tooBig {
				continue
			}
			if p.data.Len()+len(value)+1 > maxSSEEvent {
				p.tooBig = true // the event is dropped at dispatch
				p.data.Reset()
				continue
			}
			if p.hasData {
				p.data.WriteByte('\n')
			}
			p.data.WriteString(value)
			p.hasData = true
		case "id":
			if !strings.ContainsRune(value, 0) {
				p.id = value
			}
		case "retry":
			if n, err := strconv.ParseUint(value, 10, 31); err == nil && value != "" {
				p.retry = time.Duration(n) * time.Millisecond
			}
		}
	}
	if err := p.sc.Err(); err != nil {
		return sseEvent{}, err
	}
	return sseEvent{}, io.EOF
}

func (p *sseReader) dispatch() (sseEvent, bool) {
	defer func() {
		p.typ = ""
		p.data.Reset()
		p.hasData = false
		p.tooBig = false
	}()
	if !p.hasData || p.tooBig {
		return sseEvent{}, false
	}
	typ := p.typ
	if typ == "" {
		typ = "message"
	}
	ev := sseEvent{Type: typ, Data: p.data.String(), ID: p.id, Retry: p.retry}
	p.retry = 0
	return ev, true
}

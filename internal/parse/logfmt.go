// logfmt.go parses the logfmt dialect written by Go services, Heroku
// routers, and most structured-logging libraries: space-separated
// key=value pairs, values optionally double-quoted with backslash escapes,
// bare keys meaning boolean true.
package parse

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/JaydenCJ/logvert/internal/event"
)

// Logfmt parses one logfmt line. In auto-detection the boolean gate is
// strict: every token must be a well-formed pair or bare key, and at least
// one token must carry "=", so free prose like "error = disk full" is not
// misclassified. Forced mode (--format logfmt) reuses the same parse but
// tolerates a false return by the caller checking ok only in auto mode.
func Logfmt(line string, opt Options) (event.Event, bool) {
	pairs, ok := scanLogfmt(line)
	if !ok || len(pairs) == 0 {
		return event.Event{}, false
	}
	sawEq := false
	ev := event.Event{Source: event.SourceLogfmt}
	for _, p := range pairs {
		if p.hasEq {
			sawEq = true
		}
		ev.Fields.Set(p.key, p.value)
	}
	if !sawEq {
		// A line of bare words is prose, not logfmt.
		return event.Event{}, false
	}
	lift(&ev, opt)
	return ev, true
}

type logfmtPair struct {
	key   string
	value any
	hasEq bool
}

// scanLogfmt tokenizes a logfmt line. ok turns false on any malformed
// token (unterminated quote, empty key, stray quote inside a bare word),
// which is what makes the parser safe to use as a detector.
func scanLogfmt(line string) ([]logfmtPair, bool) {
	var pairs []logfmtPair
	i := 0
	n := len(line)
	for i < n {
		// Skip inter-token spaces.
		for i < n && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
		if i >= n {
			break
		}
		// Key: printable characters except space, '=', and '"'.
		start := i
		for i < n && line[i] > ' ' && line[i] != '=' && line[i] != '"' {
			i++
		}
		key := line[start:i]
		if key == "" {
			return nil, false
		}
		if i >= n || line[i] == ' ' || line[i] == '\t' {
			// Bare key: logfmt convention for boolean flags.
			pairs = append(pairs, logfmtPair{key: key, value: true})
			continue
		}
		if line[i] == '"' {
			// A quote may not begin mid-key ('foo"bar').
			return nil, false
		}
		i++ // consume '='
		if i >= n || line[i] == ' ' || line[i] == '\t' {
			// "key=" with no value: empty string.
			pairs = append(pairs, logfmtPair{key: key, value: "", hasEq: true})
			continue
		}
		if line[i] == '"' {
			val, next, ok := scanQuoted(line, i)
			if !ok {
				return nil, false
			}
			i = next
			pairs = append(pairs, logfmtPair{key: key, value: val, hasEq: true})
			continue
		}
		vstart := i
		for i < n && line[i] > ' ' && line[i] != '"' {
			i++
		}
		if i < n && line[i] == '"' {
			return nil, false
		}
		pairs = append(pairs, logfmtPair{key: key, value: typedBare(line[vstart:i]), hasEq: true})
	}
	return pairs, true
}

// scanQuoted consumes a double-quoted value starting at line[i] == '"',
// returning the unescaped value and the index after the closing quote.
func scanQuoted(line string, i int) (string, int, bool) {
	var b strings.Builder
	i++ // opening quote
	for i < len(line) {
		c := line[i]
		switch c {
		case '"':
			i++
			if i < len(line) && line[i] > ' ' {
				// Garbage glued after the closing quote ('k="v"x').
				return "", 0, false
			}
			return b.String(), i, true
		case '\\':
			i++
			if i >= len(line) {
				return "", 0, false
			}
			switch line[i] {
			case '"', '\\', '/':
				b.WriteByte(line[i])
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case 'u':
				if i+4 < len(line) {
					if r, err := strconv.ParseUint(line[i+1:i+5], 16, 32); err == nil {
						b.WriteRune(rune(r))
						i += 4
						break
					}
				}
				return "", 0, false
			default:
				// Unknown escape: keep it verbatim rather than guessing.
				b.WriteByte('\\')
				b.WriteByte(line[i])
			}
			i++
		default:
			b.WriteByte(c)
			i++
		}
	}
	return "", 0, false // unterminated quote
}

// typedBare gives unquoted logfmt values their natural JSON type: numbers
// stay numbers and true/false stay booleans, so `status=200` round-trips
// as 200, not "200". Quoted values always stay strings — the producer
// quoted them on purpose.
func typedBare(s string) any {
	switch s {
	case "true":
		return true
	case "false":
		return false
	case "null":
		return nil
	}
	if looksNumeric(s) {
		return json.Number(s)
	}
	return s
}

// looksNumeric matches JSON-compatible number syntax only; "1.2.3" or
// "15ms" must remain strings.
func looksNumeric(s string) bool {
	if s == "" {
		return false
	}
	i := 0
	if s[i] == '-' {
		i++
	}
	digits := 0
	for ; i < len(s) && s[i] >= '0' && s[i] <= '9'; i++ {
		digits++
	}
	if digits == 0 {
		return false
	}
	if i < len(s) && s[i] == '.' {
		i++
		frac := 0
		for ; i < len(s) && s[i] >= '0' && s[i] <= '9'; i++ {
			frac++
		}
		if frac == 0 {
			return false
		}
	}
	return i == len(s)
}

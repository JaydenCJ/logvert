// access.go parses HTTP server logs: the Common/Combined access-log format
// shared by Apache httpd and nginx, plus each server's distinct error-log
// dialect. No regexes — a small tokenizer that understands quotes and
// brackets is both faster and easier to reason about.
package parse

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/JaydenCJ/logvert/internal/event"
	"github.com/JaydenCJ/logvert/internal/levels"
	"github.com/JaydenCJ/logvert/internal/tsnorm"
)

// Access parses one Common or Combined Log Format line:
//
//	remote ident authuser [date] "request" status bytes ["referer" "user-agent"]
//
// The severity is derived from the HTTP status (5xx→error, 4xx→warn) so
// access logs participate in level-based filtering downstream.
func Access(line string, opt Options) (event.Event, bool) {
	toks, ok := clfTokens(line)
	if !ok || len(toks) < 7 || len(toks) > 9 {
		return event.Event{}, false
	}
	// Structural checks first: token 3 must be the bracketed date and
	// token 4 the quoted request, or this is not an access log.
	if !toks[3].bracketed || !toks[4].quoted {
		return event.Event{}, false
	}
	t, ok := tsnorm.Parse(toks[3].text, opt.TS)
	if !ok {
		return event.Event{}, false
	}
	status, err := strconv.Atoi(toks[5].text)
	if err != nil || status < 100 || status > 599 {
		return event.Event{}, false
	}
	ev := event.Event{
		Source: event.SourceAccess,
		TS:     tsnorm.Format(t),
		Level:  levels.FromHTTPStatus(status),
		Msg:    toks[4].text,
	}
	ev.Fields.Set("remote_addr", toks[0].text)
	if toks[2].text != "-" {
		ev.Fields.Set("user", toks[2].text)
	}
	if method, path, proto, ok := splitRequest(toks[4].text); ok {
		ev.Fields.Set("method", method)
		ev.Fields.Set("path", path)
		if proto != "" {
			ev.Fields.Set("proto", proto)
		}
	}
	ev.Fields.Set("status", json.Number(toks[5].text))
	if toks[6].text != "-" {
		if _, err := strconv.Atoi(toks[6].text); err == nil {
			ev.Fields.Set("bytes", json.Number(toks[6].text))
		}
	}
	// Combined-format tails must be quoted; an unquoted straggler means
	// this is some other format and silently dropping it would lose data.
	if len(toks) >= 8 {
		if !toks[7].quoted {
			return event.Event{}, false
		}
		if toks[7].text != "-" {
			ev.Fields.Set("referer", toks[7].text)
		}
	}
	if len(toks) == 9 {
		if !toks[8].quoted {
			return event.Event{}, false
		}
		if toks[8].text != "-" {
			ev.Fields.Set("user_agent", toks[8].text)
		}
	}
	return ev, true
}

type clfToken struct {
	text      string
	quoted    bool
	bracketed bool
}

// clfTokens splits a CLF line into tokens, honoring "…" quoting (with \"
// escapes, as produced by nginx's escape=default) and […] bracketing.
func clfTokens(line string) ([]clfToken, bool) {
	var toks []clfToken
	i, n := 0, len(line)
	for i < n {
		for i < n && line[i] == ' ' {
			i++
		}
		if i >= n {
			break
		}
		switch line[i] {
		case '"':
			var b strings.Builder
			i++
			closed := false
			for i < n {
				if line[i] == '\\' && i+1 < n {
					b.WriteByte(line[i+1])
					i += 2
					continue
				}
				if line[i] == '"' {
					closed = true
					i++
					break
				}
				b.WriteByte(line[i])
				i++
			}
			if !closed {
				return nil, false
			}
			toks = append(toks, clfToken{text: b.String(), quoted: true})
		case '[':
			end := strings.IndexByte(line[i:], ']')
			if end < 0 {
				return nil, false
			}
			toks = append(toks, clfToken{text: line[i+1 : i+end], bracketed: true})
			i += end + 1
		default:
			start := i
			for i < n && line[i] != ' ' {
				i++
			}
			toks = append(toks, clfToken{text: line[start:i]})
		}
	}
	return toks, true
}

// splitRequest breaks "GET /path HTTP/1.1" into its parts. Malformed
// requests (as logged for protocol-level junk) return ok=false and stay
// message-only.
func splitRequest(req string) (method, path, proto string, ok bool) {
	parts := strings.Split(req, " ")
	switch len(parts) {
	case 2:
		return parts[0], parts[1], "", isHTTPMethod(parts[0])
	case 3:
		return parts[0], parts[1], parts[2], isHTTPMethod(parts[0])
	default:
		return "", "", "", false
	}
}

func isHTTPMethod(s string) bool {
	switch s {
	case "GET", "HEAD", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "TRACE", "CONNECT", "PRI":
		return true
	}
	return false
}

// NginxError parses nginx's error-log dialect:
//
//	2026/07/12 10:00:01 [error] 4242#4242: *17 message, client: 1.2.3.4, server: example.test, request: "GET / HTTP/1.1"
//
// The trailing ", key: value" context pairs nginx appends are promoted to
// structured fields instead of staying glued to the message.
func NginxError(line string, opt Options) (event.Event, bool) {
	if len(line) < 20 || line[4] != '/' || line[7] != '/' || line[10] != ' ' {
		return event.Event{}, false
	}
	t, ok := tsnorm.Parse(line[:19], opt.TS)
	if !ok {
		return event.Event{}, false
	}
	rest := line[19:]
	if !strings.HasPrefix(rest, " [") {
		return event.Event{}, false
	}
	end := strings.IndexByte(rest, ']')
	if end < 3 {
		return event.Event{}, false
	}
	sev := rest[2:end]
	lvl, ok := levels.Normalize(sev)
	if !ok {
		return event.Event{}, false
	}
	ev := event.Event{Source: event.SourceNginxError, TS: tsnorm.Format(t), Level: lvl}
	rest = strings.TrimPrefix(rest[end+1:], " ")
	// "PID#TID: " — worker process and thread ids.
	if hash := strings.IndexByte(rest, '#'); hash > 0 {
		if colon := strings.Index(rest, ": "); colon > hash {
			pid, err1 := strconv.Atoi(rest[:hash])
			tid, err2 := strconv.Atoi(rest[hash+1 : colon])
			if err1 == nil && err2 == nil {
				ev.PID = pid
				if tid != 0 {
					ev.Fields.Set("tid", json.Number(rest[hash+1:colon]))
				}
				rest = rest[colon+2:]
			}
		}
	}
	// "*N " — the connection id nginx prefixes on request-scoped errors.
	if strings.HasPrefix(rest, "*") {
		if sp := strings.IndexByte(rest, ' '); sp > 1 {
			if _, err := strconv.Atoi(rest[1:sp]); err == nil {
				ev.Fields.Set("connection", json.Number(rest[1:sp]))
				rest = rest[sp+1:]
			}
		}
	}
	ev.Msg = extractNginxContext(rest, &ev.Fields)
	return ev, true
}

// nginxContextKeys are the context annotations nginx appends after the
// message, rightmost first in the log line.
var nginxContextKeys = []string{"client", "server", "request", "upstream", "host", "referrer", "subrequest"}

// extractNginxContext peels ", key: value" suffixes off the message and
// stores them as fields. Values may be quoted; quoted commas are safe.
func extractNginxContext(msg string, fields *event.Fields) string {
	type kv struct{ key, val string }
	var found []kv
	for {
		idx, key := lastContextKey(msg)
		if idx < 0 {
			break
		}
		val := strings.TrimSpace(msg[idx+len(", "+key+": "):])
		val = strings.Trim(val, `"`)
		found = append(found, kv{key, val})
		msg = msg[:idx]
	}
	// Peeled right-to-left; store in reading order.
	for i := len(found) - 1; i >= 0; i-- {
		fields.Set(found[i].key, found[i].val)
	}
	return msg
}

// lastContextKey finds the rightmost ", key: " marker that sits outside
// any quoted region of msg.
func lastContextKey(msg string) (int, string) {
	best := -1
	bestKey := ""
	for _, key := range nginxContextKeys {
		marker := ", " + key + ": "
		idx := strings.LastIndex(msg, marker)
		if idx > best && !insideQuotes(msg, idx) {
			best, bestKey = idx, key
		}
	}
	return best, bestKey
}

// insideQuotes reports whether position idx falls inside a "…" region.
func insideQuotes(s string, idx int) bool {
	quotes := 0
	for i := 0; i < idx; i++ {
		if s[i] == '"' {
			quotes++
		}
	}
	return quotes%2 == 1
}

// ApacheError parses Apache httpd's error-log dialect, both 2.4
// ("[Fri Jul 10 10:00:00.123456 2026] [core:error] [pid 1234:tid 5] [client 1.2.3.4:5678] AH00126: msg")
// and the older 2.2 shape without module and pid blocks.
func ApacheError(line string, opt Options) (event.Event, bool) {
	if !strings.HasPrefix(line, "[") {
		return event.Event{}, false
	}
	toks, rest, ok := leadingBrackets(line)
	if !ok || len(toks) < 2 {
		return event.Event{}, false
	}
	t, ok := tsnorm.Parse(toks[0], opt.TS)
	if !ok {
		return event.Event{}, false
	}
	ev := event.Event{Source: event.SourceApacheError, TS: tsnorm.Format(t)}
	// Second block: "module:level" (2.4) or bare "level" (2.2).
	module, sev, hasModule := strings.Cut(toks[1], ":")
	if !hasModule {
		sev = toks[1]
	}
	lvl, ok := levels.Normalize(sev)
	if !ok {
		return event.Event{}, false
	}
	ev.Level = lvl
	if hasModule && module != "" {
		ev.Fields.Set("module", module)
	}
	for _, tok := range toks[2:] {
		switch {
		case strings.HasPrefix(tok, "pid "):
			body := strings.TrimPrefix(tok, "pid ")
			pidStr, tidStr, hasTid := strings.Cut(body, ":tid ")
			if n, err := strconv.Atoi(pidStr); err == nil {
				ev.PID = n
			}
			if hasTid {
				if _, err := strconv.Atoi(tidStr); err == nil {
					ev.Fields.Set("tid", json.Number(tidStr))
				}
			}
		case strings.HasPrefix(tok, "client "):
			ev.Fields.Set("client", strings.TrimPrefix(tok, "client "))
		}
	}
	// "AHnnnnn:" — Apache's stable error code, worth indexing on.
	if len(rest) > 8 && strings.HasPrefix(rest, "AH") {
		if colon := strings.IndexByte(rest, ':'); colon >= 7 && colon <= 8 {
			if _, err := strconv.Atoi(rest[2:colon]); err == nil {
				ev.Fields.Set("code", rest[:colon])
				rest = strings.TrimPrefix(rest[colon+1:], " ")
			}
		}
	}
	ev.Msg = rest
	return ev, true
}

// leadingBrackets consumes the run of "[…]" blocks that opens an Apache
// error line and returns their contents plus the remaining message.
func leadingBrackets(line string) ([]string, string, bool) {
	var toks []string
	for strings.HasPrefix(line, "[") {
		end := strings.IndexByte(line, ']')
		if end < 0 {
			return nil, "", false
		}
		toks = append(toks, line[1:end])
		line = strings.TrimPrefix(line[end+1:], " ")
	}
	return toks, line, true
}

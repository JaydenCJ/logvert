// syslog.go parses both syslog generations: RFC 5424 (versioned header,
// structured data) and RFC 3164 / BSD (the "Jan  2 15:04:05 host tag[pid]:"
// shape still written by rsyslog and most appliances), with or without the
// leading <PRI> priority field.
package parse

import (
	"strconv"
	"strings"

	"github.com/JaydenCJ/logvert/internal/event"
	"github.com/JaydenCJ/logvert/internal/levels"
	"github.com/JaydenCJ/logvert/internal/tsnorm"
)

// Syslog parses one syslog line of either RFC.
func Syslog(line string, opt Options) (event.Event, bool) {
	pri, rest, hasPri := scanPri(line)
	if strings.HasPrefix(rest, "1 ") {
		if ev, ok := syslog5424(rest[2:], opt); ok {
			applyPri(&ev, pri, hasPri)
			return ev, true
		}
		return event.Event{}, false
	}
	if ev, ok := syslog3164(rest, opt); ok {
		applyPri(&ev, pri, hasPri)
		return ev, true
	}
	return event.Event{}, false
}

// scanPri consumes an optional leading "<NNN>" priority (0–191).
func scanPri(line string) (pri int, rest string, ok bool) {
	if len(line) < 3 || line[0] != '<' {
		return 0, line, false
	}
	end := strings.IndexByte(line[:min(len(line), 6)], '>')
	if end < 2 {
		return 0, line, false
	}
	n, err := strconv.Atoi(line[1:end])
	if err != nil || n < 0 || n > 191 {
		return 0, line, false
	}
	return n, line[end+1:], true
}

// applyPri decodes PRI into level (severity) and fields.facility.
func applyPri(ev *event.Event, pri int, hasPri bool) {
	if !hasPri {
		return
	}
	if lvl, ok := levels.FromSyslogSeverity(pri & 7); ok && ev.Level == "" {
		ev.Level = lvl
	}
	if name, ok := levels.FacilityName(pri >> 3); ok {
		// Prepend facility so it precedes any structured-data fields.
		ev.Fields = append(event.Fields{{Key: "facility", Value: name}}, ev.Fields...)
	}
}

// syslog5424 parses the RFC 5424 body after "<PRI>1 ":
// TIMESTAMP HOSTNAME APP-NAME PROCID MSGID SD [MSG]. "-" means nil.
func syslog5424(rest string, opt Options) (event.Event, bool) {
	ev := event.Event{Source: event.SourceSyslog}
	fields := [5]string{}
	for i := 0; i < 5; i++ {
		tok, r, ok := cutToken(rest)
		if !ok {
			return event.Event{}, false
		}
		fields[i], rest = tok, r
	}
	ts, host, app, procid, msgid := fields[0], fields[1], fields[2], fields[3], fields[4]
	if ts != "-" {
		t, ok := tsnorm.Parse(ts, opt.TS)
		if !ok {
			return event.Event{}, false
		}
		ev.TS = tsnorm.Format(t)
	}
	if host != "-" {
		ev.Host = host
	}
	if app != "-" {
		ev.App = app
	}
	if procid != "-" {
		if n, err := strconv.Atoi(procid); err == nil && n > 0 {
			ev.PID = n
		} else {
			ev.Fields.Set("procid", procid)
		}
	}
	if msgid != "-" {
		ev.Fields.Set("msgid", msgid)
	}
	// Structured data: "-" or one or more [SD-ID param="value" ...] blocks.
	rest, ok := scanSD(rest, &ev)
	if !ok {
		return event.Event{}, false
	}
	// RFC 5424 §6.4 allows a UTF-8 BOM before the free-text message.
	ev.Msg = strings.TrimPrefix(strings.TrimPrefix(rest, " "), "\uFEFF")
	return ev, true
}

// cutToken splits the next space-delimited token off rest.
func cutToken(rest string) (tok, remainder string, ok bool) {
	if rest == "" {
		return "", "", false
	}
	if i := strings.IndexByte(rest, ' '); i >= 0 {
		return rest[:i], rest[i+1:], rest[:i] != ""
	}
	return rest, "", true
}

// scanSD consumes the structured-data element(s). Params are flattened
// into fields as "<sd-id>.<param>" so two SD blocks can never collide.
func scanSD(rest string, ev *event.Event) (string, bool) {
	if rest == "-" || strings.HasPrefix(rest, "- ") {
		return strings.TrimPrefix(rest, "-"), true
	}
	for strings.HasPrefix(rest, "[") {
		end, ok := sdBlockEnd(rest)
		if !ok {
			return "", false
		}
		if !parseSDBlock(rest[1:end], ev) {
			return "", false
		}
		rest = rest[end+1:]
	}
	return rest, true
}

// sdBlockEnd finds the index of the unescaped ']' closing the SD block
// that starts at rest[0] == '['.
func sdBlockEnd(rest string) (int, bool) {
	inQuote := false
	for i := 1; i < len(rest); i++ {
		switch rest[i] {
		case '\\':
			i++ // skip escaped char
		case '"':
			inQuote = !inQuote
		case ']':
			if !inQuote {
				return i, true
			}
		}
	}
	return 0, false
}

// parseSDBlock parses `SD-ID key="value" ...` (the brackets already
// stripped) into ev.Fields.
func parseSDBlock(body string, ev *event.Event) bool {
	sdid, rest, _ := strings.Cut(body, " ")
	if sdid == "" {
		return false
	}
	for rest != "" {
		rest = strings.TrimLeft(rest, " ")
		if rest == "" {
			break
		}
		eq := strings.IndexByte(rest, '=')
		if eq <= 0 || len(rest) < eq+2 || rest[eq+1] != '"' {
			return false
		}
		key := rest[:eq]
		val, next, ok := sdValue(rest[eq+2:])
		if !ok {
			return false
		}
		ev.Fields.Set(sdid+"."+key, val)
		rest = next
	}
	return true
}

// sdValue reads an SD param value up to its closing quote, applying the
// three escapes RFC 5424 defines: \" \\ \].
func sdValue(rest string) (val, remainder string, ok bool) {
	var b strings.Builder
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case '\\':
			if i+1 < len(rest) {
				i++
				switch rest[i] {
				case '"', '\\', ']':
					b.WriteByte(rest[i])
				default:
					b.WriteByte('\\')
					b.WriteByte(rest[i])
				}
			}
		case '"':
			return b.String(), rest[i+1:], true
		default:
			b.WriteByte(rest[i])
		}
	}
	return "", "", false
}

// syslog3164 parses the BSD shape: "Jan  2 15:04:05 host tag[pid]: msg".
func syslog3164(rest string, opt Options) (event.Event, bool) {
	// The timestamp is fixed-width: "Mmm dd hh:mm:ss" = 15 bytes.
	if len(rest) < 16 || rest[15] != ' ' {
		return event.Event{}, false
	}
	t, ok := tsnorm.Parse(rest[:15], opt.TS)
	if !ok {
		return event.Event{}, false
	}
	ev := event.Event{Source: event.SourceSyslog, TS: tsnorm.Format(t)}
	rest = rest[16:]
	host, rest, ok := cutToken(rest)
	if !ok || host == "" {
		return event.Event{}, false
	}
	ev.Host = host
	ev.App, ev.PID, ev.Msg = splitTag(rest)
	return ev, true
}

// splitTag extracts the RFC 3164 TAG ("app", "app[pid]", "app:") from the
// start of the content. Lines whose content carries no tag keep everything
// as the message.
func splitTag(content string) (app string, pid int, msg string) {
	i := 0
	for i < len(content) && isTagByte(content[i]) {
		i++
	}
	if i == 0 || i > 48 {
		return "", 0, content
	}
	app = content[:i]
	rest := content[i:]
	if strings.HasPrefix(rest, "[") {
		end := strings.IndexByte(rest, ']')
		if end > 1 {
			if n, err := strconv.Atoi(rest[1:end]); err == nil && n > 0 {
				pid = n
				rest = rest[end+1:]
			}
		}
	}
	if !strings.HasPrefix(rest, ":") {
		// No colon after tag[pid] — this was not a tag after all.
		return "", 0, content
	}
	return app, pid, strings.TrimPrefix(strings.TrimPrefix(rest, ":"), " ")
}

// isTagByte reports whether c may appear in an RFC 3164 TAG. The RFC says
// alphanumerics; real programs add '-', '_', '.', and '/'.
func isTagByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	case c == '-' || c == '_' || c == '.' || c == '/':
		return true
	}
	return false
}

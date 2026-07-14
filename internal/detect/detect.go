// Package detect classifies each input line and dispatches it to the right
// parser. Detection is attempted-parse, not regex sniffing: a line counts
// as a format only if the full parser for that format accepts it, so a
// misdetection can never produce a half-parsed event.
package detect

import (
	"fmt"

	"github.com/JaydenCJ/logvert/internal/event"
	"github.com/JaydenCJ/logvert/internal/parse"
)

// Formats accepted by --format, in the order auto tries them. JSON first
// (cheapest to reject), then the three unambiguous line dialects, then
// logfmt, whose strict token gate makes it a safe near-last resort.
var Formats = []string{
	event.SourceJSON,
	event.SourceSyslog,
	event.SourceNginxError,
	event.SourceApacheError,
	event.SourceAccess,
	event.SourceLogfmt,
}

type parserFunc func(string, parse.Options) (event.Event, bool)

var parsers = map[string]parserFunc{
	event.SourceJSON:        parse.JSONLine,
	event.SourceSyslog:      parse.Syslog,
	event.SourceNginxError:  parse.NginxError,
	event.SourceApacheError: parse.ApacheError,
	event.SourceAccess:      parse.Access,
	event.SourceLogfmt:      parse.Logfmt,
}

// ValidFormat reports whether name is "auto", "raw", or a parser name.
func ValidFormat(name string) bool {
	if name == "auto" || name == event.SourceRaw {
		return true
	}
	_, ok := parsers[name]
	return ok
}

// Line converts one input line to an event. format is "auto", "raw", or a
// specific parser name (already validated). Lines no parser accepts come
// back as a raw event carrying the whole line as the message, so nothing
// is ever silently dropped.
func Line(line, format string, opt parse.Options) event.Event {
	switch format {
	case "auto":
		for _, name := range Formats {
			if ev, ok := parsers[name](line, opt); ok {
				return ev
			}
		}
	case event.SourceRaw:
		// fall through to the raw event below
	default:
		if p, ok := parsers[format]; ok {
			if ev, ok := p(line, opt); ok {
				return ev
			}
		}
	}
	return event.Event{Msg: line, Source: event.SourceRaw}
}

// FormatList renders the accepted --format values for usage text.
func FormatList() string {
	return fmt.Sprintf("auto, %s, %s, %s, %s, %s, %s, raw",
		Formats[0], Formats[1], Formats[2], Formats[3], Formats[4], Formats[5])
}

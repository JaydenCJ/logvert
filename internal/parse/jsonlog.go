// jsonlog.go parses one-object-per-line JSON logs (zap, slog, pino,
// bunyan, Docker json-file, …). A token-level decode preserves the
// producer's key order and passes nested values through verbatim as
// json.RawMessage, so nothing is re-shaped except the canonical envelope.
package parse

import (
	"encoding/json"
	"strings"

	"github.com/JaydenCJ/logvert/internal/event"
)

// JSONLine parses one JSON log line. Only a top-level object counts as a
// log record; arrays and scalars are legal JSON but not log events.
func JSONLine(line string, opt Options) (event.Event, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "{") {
		return event.Event{}, false
	}
	dec := json.NewDecoder(strings.NewReader(trimmed))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return event.Event{}, false
	}
	ev := event.Event{Source: event.SourceJSON}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return event.Event{}, false
		}
		key, ok := keyTok.(string)
		if !ok {
			return event.Event{}, false
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return event.Event{}, false
		}
		ev.Fields.Set(key, json.RawMessage(raw))
	}
	if _, err := dec.Token(); err != nil { // closing '}'
		return event.Event{}, false
	}
	// Trailing garbage after the object means the line was not JSON.
	if dec.More() {
		return event.Event{}, false
	}
	lift(&ev, opt)
	return ev, true
}

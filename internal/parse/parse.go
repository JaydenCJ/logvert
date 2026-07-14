// Package parse implements logvert's built-in parsers: logfmt, syslog
// (RFC 3164 and RFC 5424), Apache/nginx access and error logs, and JSON
// lines. Each parser is pure — string in, Event out — so every format rule
// is unit-testable without I/O.
package parse

import (
	"encoding/json"
	"strconv"

	"github.com/JaydenCJ/logvert/internal/event"
	"github.com/JaydenCJ/logvert/internal/levels"
	"github.com/JaydenCJ/logvert/internal/tsnorm"
)

// Options carry the assumptions shared by all parsers.
type Options struct {
	TS tsnorm.Options
}

// Canonical key aliases, in priority order: the first alias present in a
// structured line (logfmt or JSON) is lifted into the envelope; later
// aliases stay in fields under their own names.
var (
	tsKeys    = []string{"ts", "time", "timestamp", "@timestamp", "datetime"}
	levelKeys = []string{"level", "lvl", "severity", "loglevel"}
	msgKeys   = []string{"msg", "message"}
	hostKeys  = []string{"host", "hostname"}
	appKeys   = []string{"app", "service", "logger", "program", "name"}
	pidKeys   = []string{"pid"}
)

// lift moves canonical fields out of ev.Fields into the envelope, applying
// timestamp and level normalization. It is shared by the logfmt and JSON
// parsers; line-format parsers (syslog, access) fill the envelope directly.
func lift(ev *event.Event, opt Options) {
	if key, v, ok := firstOf(ev.Fields, tsKeys); ok {
		if ts, ok := coerceTime(v, opt.TS); ok {
			ev.TS = ts
			ev.Fields.Drop(key)
		}
	}
	if key, v, ok := firstOf(ev.Fields, levelKeys); ok {
		if lvl, raw, ok := coerceLevel(v); ok {
			ev.Level = lvl
			ev.Fields.Drop(key)
			if raw != "" {
				// Unrecognized spelling: keep the original so nothing is lost.
				ev.Fields.Set("level_raw", raw)
			}
		}
	}
	if key, v, ok := firstOf(ev.Fields, msgKeys); ok {
		if s, ok := coerceString(v); ok {
			ev.Msg = s
			ev.Fields.Drop(key)
		}
	}
	if key, v, ok := firstOf(ev.Fields, hostKeys); ok {
		if s, ok := coerceString(v); ok && s != "" {
			ev.Host = s
			ev.Fields.Drop(key)
		}
	}
	if key, v, ok := firstOf(ev.Fields, appKeys); ok {
		if s, ok := coerceString(v); ok && s != "" {
			ev.App = s
			ev.Fields.Drop(key)
		}
	}
	if key, v, ok := firstOf(ev.Fields, pidKeys); ok {
		if n, ok := coerceInt(v); ok && n > 0 {
			ev.PID = n
			ev.Fields.Drop(key)
		}
	}
}

// LiftValue moves one value into the named envelope slot, applying the
// same normalization as automatic lifting. It backs the --map flag when
// the target is canonical. Reports whether the value was accepted.
func LiftValue(ev *event.Event, target string, v any, opt Options) bool {
	switch target {
	case "ts":
		if ts, ok := coerceTime(v, opt.TS); ok {
			ev.TS = ts
			return true
		}
	case "level":
		if lvl, raw, ok := coerceLevel(v); ok {
			ev.Level = lvl
			if raw != "" {
				ev.Fields.Set("level_raw", raw)
			}
			return true
		}
	case "msg":
		if s, ok := coerceString(v); ok {
			ev.Msg = s
			return true
		}
	case "host":
		if s, ok := coerceString(v); ok && s != "" {
			ev.Host = s
			return true
		}
	case "app":
		if s, ok := coerceString(v); ok && s != "" {
			ev.App = s
			return true
		}
	case "pid":
		if n, ok := coerceInt(v); ok && n > 0 {
			ev.PID = n
			return true
		}
	}
	return false
}

// firstOf returns the first key from keys present in fields.
func firstOf(fields event.Fields, keys []string) (string, any, bool) {
	for _, k := range keys {
		if v, ok := fields.Get(k); ok {
			return k, v, true
		}
	}
	return "", nil, false
}

// coerceTime accepts a string in any tsnorm dialect, an epoch number, or a
// JSON raw value holding either, and returns canonical RFC 3339 UTC.
func coerceTime(v any, opt tsnorm.Options) (string, bool) {
	switch x := v.(type) {
	case string:
		if t, ok := tsnorm.Parse(x, opt); ok {
			return tsnorm.Format(t), true
		}
	case json.Number:
		if t, ok := tsnorm.ParseEpoch(x.String()); ok {
			return tsnorm.Format(t), true
		}
	case json.RawMessage:
		var s string
		if err := json.Unmarshal(x, &s); err == nil {
			return coerceTime(s, opt)
		}
		var n json.Number
		if err := json.Unmarshal(x, &n); err == nil {
			return coerceTime(n, opt)
		}
	}
	return "", false
}

// coerceLevel normalizes a level value. When the spelling is unknown it
// still lifts (ok=true) but hands back the raw spelling for preservation.
func coerceLevel(v any) (lvl, raw string, ok bool) {
	switch x := v.(type) {
	case string:
		if l, ok := levels.Normalize(x); ok {
			return l, "", true
		}
		return "", x, true
	case json.Number:
		if n, err := strconv.Atoi(x.String()); err == nil {
			if l, ok := levels.FromNumeric(n); ok {
				return l, "", true
			}
		}
		return "", x.String(), true
	case json.RawMessage:
		var s string
		if err := json.Unmarshal(x, &s); err == nil {
			return coerceLevel(s)
		}
		var n json.Number
		if err := json.Unmarshal(x, &n); err == nil {
			return coerceLevel(n)
		}
	}
	return "", "", false
}

func coerceString(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case json.RawMessage:
		var s string
		if err := json.Unmarshal(x, &s); err == nil {
			return s, true
		}
	}
	return "", false
}

func coerceInt(v any) (int, bool) {
	switch x := v.(type) {
	case json.Number:
		if n, err := strconv.Atoi(x.String()); err == nil {
			return n, true
		}
	case string:
		if n, err := strconv.Atoi(x); err == nil {
			return n, true
		}
	case json.RawMessage:
		var n json.Number
		if err := json.Unmarshal(x, &n); err == nil {
			return coerceInt(n)
		}
	}
	return 0, false
}

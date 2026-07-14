// Package event defines logvert's normalized event envelope and its
// deterministic JSONL encoding. Every parser produces an Event; the encoder
// guarantees a stable key order so identical input always yields
// byte-identical output.
package event

import (
	"bytes"
	"encoding/json"
)

// Source values, one per built-in parser plus the raw fallback.
const (
	SourceJSON        = "json"
	SourceLogfmt      = "logfmt"
	SourceSyslog      = "syslog"
	SourceAccess      = "access"
	SourceApacheError = "apache-error"
	SourceNginxError  = "nginx-error"
	SourceRaw         = "raw"
)

// EnvelopeKeys are the reserved top-level keys of the normalized schema, in
// output order. Extra fields may not shadow them in --flat mode.
var EnvelopeKeys = []string{"ts", "level", "msg", "host", "app", "pid", "source"}

// Event is one normalized log record. Zero values ("" and 0) mean "absent"
// and are omitted from the encoded output, except Msg and Source which are
// always emitted.
type Event struct {
	TS     string // RFC 3339 UTC, or "" when the line carried no timestamp
	Level  string // normalized level (trace..fatal), or ""
	Msg    string
	Host   string
	App    string // program / service / logger name
	PID    int    // 0 = absent
	Source string
	Fields Fields
}

// Field is one extra key/value pair that survived canonical lifting.
// Value is a string, bool, json.Number, or json.RawMessage (JSON input
// passes nested values through verbatim).
type Field struct {
	Key   string
	Value any
}

// Fields preserves the order keys appeared in the source line, which keeps
// output stable and human-diffable.
type Fields []Field

// Set appends key=v, replacing the value in place if key already exists.
func (f *Fields) Set(key string, v any) {
	for i := range *f {
		if (*f)[i].Key == key {
			(*f)[i].Value = v
			return
		}
	}
	*f = append(*f, Field{Key: key, Value: v})
}

// Get returns the value for key and whether it was present.
func (f Fields) Get(key string) (any, bool) {
	for i := range f {
		if f[i].Key == key {
			return f[i].Value, true
		}
	}
	return nil, false
}

// Drop removes key if present and reports whether it did.
func (f *Fields) Drop(key string) bool {
	for i := range *f {
		if (*f)[i].Key == key {
			*f = append((*f)[:i], (*f)[i+1:]...)
			return true
		}
	}
	return false
}

// Rename changes the key of an existing field, keeping its position. If the
// new key already exists, the renamed field wins and the old one is dropped.
func (f *Fields) Rename(from, to string) bool {
	for i := range *f {
		if (*f)[i].Key == from {
			(*f)[i].Key = to
			for j := range *f {
				if j != i && (*f)[j].Key == to {
					*f = append((*f)[:j], (*f)[j+1:]...)
					break
				}
			}
			return true
		}
	}
	return false
}

// isEnvelopeKey reports whether k collides with a reserved envelope key.
func isEnvelopeKey(k string) bool {
	for _, e := range EnvelopeKeys {
		if k == e {
			return true
		}
	}
	return false
}

// Encode renders the event as one JSONL line (no trailing newline).
// Envelope keys come first in fixed order, then extra fields in source
// order. With flat=false extras nest under "fields"; with flat=true they
// merge into the top level, and a key that would shadow an envelope key is
// emitted with a "_" prefix instead of silently dropping data.
func Encode(e Event, flat bool) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	first := true
	emit := func(key string, v any) error {
		if !first {
			buf.WriteByte(',')
		}
		first = false
		// Keys must go through the JSON encoder too: Go's strconv.Quote
		// writes \x escapes that are not legal JSON.
		if err := appendJSON(&buf, key); err != nil {
			return err
		}
		buf.WriteByte(':')
		return appendJSON(&buf, v)
	}
	if e.TS != "" {
		if err := emit("ts", e.TS); err != nil {
			return nil, err
		}
	}
	if e.Level != "" {
		if err := emit("level", e.Level); err != nil {
			return nil, err
		}
	}
	if err := emit("msg", e.Msg); err != nil {
		return nil, err
	}
	if e.Host != "" {
		if err := emit("host", e.Host); err != nil {
			return nil, err
		}
	}
	if e.App != "" {
		if err := emit("app", e.App); err != nil {
			return nil, err
		}
	}
	if e.PID != 0 {
		if err := emit("pid", e.PID); err != nil {
			return nil, err
		}
	}
	if err := emit("source", e.Source); err != nil {
		return nil, err
	}
	switch {
	case len(e.Fields) == 0:
		// nothing to add
	case flat:
		for _, f := range e.Fields {
			key := f.Key
			if isEnvelopeKey(key) {
				key = "_" + key
			}
			if err := emit(key, f.Value); err != nil {
				return nil, err
			}
		}
	default:
		if !first {
			buf.WriteByte(',')
		}
		buf.WriteString(`"fields":{`)
		for i, f := range e.Fields {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := appendJSON(&buf, f.Key); err != nil {
				return nil, err
			}
			buf.WriteByte(':')
			if err := appendJSON(&buf, f.Value); err != nil {
				return nil, err
			}
		}
		buf.WriteByte('}')
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// appendJSON writes v as compact JSON without HTML escaping, so URLs and
// query strings in access logs stay readable.
func appendJSON(buf *bytes.Buffer, v any) error {
	if raw, ok := v.(json.RawMessage); ok {
		// Pass JSON-source values through verbatim but compacted, which
		// preserves nested key order exactly as the producer wrote it.
		return json.Compact(buf, raw)
	}
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return err
	}
	// Encoder appends a newline; JSONL wants exactly one record per line.
	buf.Truncate(buf.Len() - 1)
	return nil
}

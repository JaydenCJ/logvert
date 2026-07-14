// Tests for the normalized envelope encoding: fixed key order, correct
// omission of absent values, and the two fields layouts (nested and flat).
package event

import (
	"encoding/json"
	"testing"
)

func encode(t *testing.T, e Event, flat bool) string {
	t.Helper()
	b, err := Encode(e, flat)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return string(b)
}

func TestEncodeFullEnvelopeKeyOrder(t *testing.T) {
	e := Event{
		TS: "2026-07-12T10:00:00Z", Level: "info", Msg: "hello",
		Host: "web1", App: "api", PID: 42, Source: SourceLogfmt,
	}
	want := `{"ts":"2026-07-12T10:00:00Z","level":"info","msg":"hello","host":"web1","app":"api","pid":42,"source":"logfmt"}`
	if got := encode(t, e, false); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestEncodeOmitsAbsentEnvelopeValues(t *testing.T) {
	// msg and source always appear; everything else is optional.
	e := Event{Msg: "bare", Source: SourceRaw}
	if got := encode(t, e, false); got != `{"msg":"bare","source":"raw"}` {
		t.Fatalf("got %s", got)
	}
}

func TestEncodeFieldsPreserveSourceOrder(t *testing.T) {
	e := Event{Msg: "m", Source: SourceLogfmt}
	e.Fields.Set("zebra", "1")
	e.Fields.Set("alpha", "2")
	want := `{"msg":"m","source":"logfmt","fields":{"zebra":"1","alpha":"2"}}`
	if got := encode(t, e, false); got != want {
		t.Fatalf("got %s", got)
	}
}

func TestEncodeFlatMergesFieldsTopLevel(t *testing.T) {
	e := Event{Msg: "m", Source: SourceLogfmt}
	e.Fields.Set("status", json.Number("200"))
	want := `{"msg":"m","source":"logfmt","status":200}`
	if got := encode(t, e, true); got != want {
		t.Fatalf("got %s", got)
	}
}

func TestEncodeFlatPrefixesEnvelopeCollisions(t *testing.T) {
	// A field literally named "source" must not shadow the envelope key,
	// and must not be dropped either.
	e := Event{Msg: "m", Source: SourceLogfmt}
	e.Fields.Set("source", "billing-api")
	want := `{"msg":"m","source":"logfmt","_source":"billing-api"}`
	if got := encode(t, e, true); got != want {
		t.Fatalf("got %s", got)
	}
}

func TestEncodeDoesNotEscapeHTMLInStrings(t *testing.T) {
	// URLs with & and <> must survive readable for grep/jq pipelines.
	e := Event{Msg: "GET /x?a=1&b=<2>", Source: SourceAccess}
	if got := encode(t, e, false); got != `{"msg":"GET /x?a=1&b=<2>","source":"access"}` {
		t.Fatalf("got %s", got)
	}
}

func TestEncodeRawMessagePassesThroughVerbatim(t *testing.T) {
	// Nested JSON from a json-source line keeps its producer key order.
	e := Event{Msg: "m", Source: SourceJSON}
	e.Fields.Set("ctx", json.RawMessage(`{"z":1,"a":[true,null]}`))
	want := `{"msg":"m","source":"json","fields":{"ctx":{"z":1,"a":[true,null]}}}`
	if got := encode(t, e, false); got != want {
		t.Fatalf("got %s", got)
	}
}

func TestEncodeControlCharacterKeysStayValidJSON(t *testing.T) {
	// Regression: keys used to be quoted with strconv.Quote, whose \x01
	// escape is legal Go but not legal JSON, corrupting the output stream
	// when a JSON producer emits a key with a control character.
	e := Event{Msg: "m", Source: SourceJSON}
	e.Fields.Set("a\x01b", json.Number("1"))
	for _, flat := range []bool{false, true} {
		got := encode(t, e, flat)
		if !json.Valid([]byte(got)) {
			t.Fatalf("flat=%v: output is not valid JSON: %s", flat, got)
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(got), &decoded); err != nil {
			t.Fatalf("flat=%v: %v", flat, err)
		}
	}
}

func TestFieldsSetGetDropRename(t *testing.T) {
	var f Fields
	f.Set("a", 1)
	f.Set("b", 2)
	f.Set("a", 3) // replace in place, keep position
	if v, ok := f.Get("a"); !ok || v != 3 {
		t.Fatalf("Get(a) = %v, %v", v, ok)
	}
	if len(f) != 2 || f[0].Key != "a" {
		t.Fatalf("Set must replace in place: %+v", f)
	}
	if !f.Rename("a", "b") || len(f) != 1 || f[0].Key != "b" || f[0].Value != 3 {
		t.Fatalf("Rename collision must keep the renamed value: %+v", f)
	}
	if !f.Drop("b") || len(f) != 0 {
		t.Fatalf("Drop failed: %+v", f)
	}
	if f.Drop("missing") {
		t.Fatal("Drop(missing) must report false")
	}
}

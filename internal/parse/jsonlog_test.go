// Tests for the JSON-lines parser: order-preserving decode, canonical
// lifting across logger conventions (zap, pino, Docker), and rejection of
// JSON that is not a single object.
package parse

import (
	"encoding/json"
	"testing"

	"github.com/JaydenCJ/logvert/internal/event"
)

func TestJSONLineZapStyle(t *testing.T) {
	line := `{"level":"error","ts":"2026-07-12T10:00:00Z","msg":"write failed","logger":"store","attempt":3}`
	ev, ok := JSONLine(line, optTest)
	if !ok {
		t.Fatal("parse failed")
	}
	if ev.TS != "2026-07-12T10:00:00Z" || ev.Level != "error" || ev.Msg != "write failed" || ev.App != "store" {
		t.Fatalf("envelope = %+v", ev)
	}
	if v, _ := ev.Fields.Get("attempt"); string(v.(json.RawMessage)) != "3" {
		t.Fatalf("attempt = %#v", v)
	}
}

func TestJSONLinePinoNumericLevelAndEpochMillis(t *testing.T) {
	line := `{"level":30,"time":1783850400123,"pid":77,"hostname":"api-1","name":"billing","msg":"charged"}`
	ev, ok := JSONLine(line, optTest)
	if !ok {
		t.Fatal("parse failed")
	}
	if ev.Level != "info" {
		t.Fatalf("level = %q (pino 30 must map to info)", ev.Level)
	}
	if ev.TS != "2026-07-12T10:00:00.123Z" {
		t.Fatalf("ts = %q", ev.TS)
	}
	if ev.PID != 77 || ev.Host != "api-1" || ev.App != "billing" {
		t.Fatalf("envelope = %+v", ev)
	}
}

func TestJSONLineFieldOrderPreserved(t *testing.T) {
	// Producer key order must survive into the encoded output.
	line := `{"msg":"m","zebra":1,"alpha":2}`
	ev, _ := JSONLine(line, optTest)
	b, err := event.Encode(ev, false)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"msg":"m","source":"json","fields":{"zebra":1,"alpha":2}}`
	if string(b) != want {
		t.Fatalf("got  %s\nwant %s", b, want)
	}
}

func TestJSONLineNestedValuesPassThroughVerbatim(t *testing.T) {
	line := `{"msg":"m","ctx":{"b":1,"a":{"deep":[1,2,3]}}}`
	ev, _ := JSONLine(line, optTest)
	v, _ := ev.Fields.Get("ctx")
	if string(v.(json.RawMessage)) != `{"b":1,"a":{"deep":[1,2,3]}}` {
		t.Fatalf("ctx = %s", v)
	}
}

func TestJSONLineBigIntegerSurvives(t *testing.T) {
	// UseNumber keeps 64-bit ids exact; float64 would corrupt them.
	line := `{"msg":"m","span_id":9007199254740993}`
	ev, _ := JSONLine(line, optTest)
	b, _ := event.Encode(ev, false)
	if string(b) != `{"msg":"m","source":"json","fields":{"span_id":9007199254740993}}` {
		t.Fatalf("got %s", b)
	}
}

func TestJSONLineUnknownLevelKeptRaw(t *testing.T) {
	ev, _ := JSONLine(`{"level":"weird","msg":"m"}`, optTest)
	if ev.Level != "" {
		t.Fatalf("level = %q", ev.Level)
	}
	if v, _ := ev.Fields.Get("level_raw"); string(v.(string)) != "weird" {
		t.Fatalf("level_raw = %#v", v)
	}
}

func TestJSONLineUnparseableTimestampStaysField(t *testing.T) {
	// A "time" value we cannot read must remain in fields, not vanish.
	ev, _ := JSONLine(`{"time":"half past nine","msg":"m"}`, optTest)
	if ev.TS != "" {
		t.Fatalf("ts = %q", ev.TS)
	}
	if _, present := ev.Fields.Get("time"); !present {
		t.Fatal("unparseable time must stay in fields")
	}
}

func TestJSONLineRejectsNonObjectsAndJunk(t *testing.T) {
	for _, line := range []string{
		`[1,2,3]`,          // array, not a record
		`"just a string"`,  // scalar
		`{"a":1} trailing`, // garbage after the object
		`{"a":1}{"b":2}`,   // two objects on one line
		`{"a":`,            // truncated
		`{broken}`,         // not JSON at all
	} {
		if _, ok := JSONLine(line, optTest); ok {
			t.Fatalf("JSONLine(%q) unexpectedly succeeded", line)
		}
	}
}

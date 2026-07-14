// Tests for the logfmt parser: pair scanning, quoting, typing, canonical
// lifting, and the strict detection gate that keeps prose out.
package parse

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/JaydenCJ/logvert/internal/tsnorm"
)

// optTest pins timestamp assumptions for all parser tests.
var optTest = Options{TS: tsnorm.Options{Loc: time.UTC, Year: 2026}}

func TestLogfmtBasicPairs(t *testing.T) {
	ev, ok := Logfmt(`level=info msg=started port=8080`, optTest)
	if !ok {
		t.Fatal("parse failed")
	}
	if ev.Level != "info" || ev.Msg != "started" {
		t.Fatalf("lift failed: %+v", ev)
	}
	if v, _ := ev.Fields.Get("port"); v != json.Number("8080") {
		t.Fatalf("port = %#v", v)
	}
}

func TestLogfmtQuotedValuesWithEscapes(t *testing.T) {
	ev, ok := Logfmt(`msg="user \"bob\" logged in\nsecond line" path="/a b"`, optTest)
	if !ok {
		t.Fatal("parse failed")
	}
	if ev.Msg != "user \"bob\" logged in\nsecond line" {
		t.Fatalf("msg = %q", ev.Msg)
	}
	if v, _ := ev.Fields.Get("path"); v != "/a b" {
		t.Fatalf("path = %#v", v)
	}
}

func TestLogfmtBareKeyIsBooleanTrue(t *testing.T) {
	// `key` with no '=' is the logfmt flag convention.
	ev, ok := Logfmt(`cached msg=hit`, optTest)
	if !ok {
		t.Fatal("parse failed")
	}
	if v, _ := ev.Fields.Get("cached"); v != true {
		t.Fatalf("cached = %#v", v)
	}
}

func TestLogfmtEmptyValue(t *testing.T) {
	ev, ok := Logfmt(`msg=done user= status=204`, optTest)
	if !ok {
		t.Fatal("parse failed")
	}
	if v, _ := ev.Fields.Get("user"); v != "" {
		t.Fatalf("user = %#v", v)
	}
}

func TestLogfmtBareValueTyping(t *testing.T) {
	// Unquoted numbers/bools get JSON types; anything else stays string.
	ev, ok := Logfmt(`msg=m n=-3.5 ok=true off=false nil=null dur=15ms ver=1.2.3`, optTest)
	if !ok {
		t.Fatal("parse failed")
	}
	if v, _ := ev.Fields.Get("n"); v != json.Number("-3.5") {
		t.Fatalf("n = %#v", v)
	}
	if v, _ := ev.Fields.Get("ok"); v != true {
		t.Fatalf("ok = %#v", v)
	}
	if v, _ := ev.Fields.Get("off"); v != false {
		t.Fatalf("off = %#v", v)
	}
	if v, _ := ev.Fields.Get("nil"); v != nil {
		t.Fatalf("nil = %#v", v)
	}
	if v, _ := ev.Fields.Get("dur"); v != "15ms" {
		t.Fatalf("dur = %#v", v)
	}
	if v, _ := ev.Fields.Get("ver"); v != "1.2.3" {
		t.Fatalf("ver = %#v", v)
	}
	// A quoted number stays a string — the producer quoted it on purpose.
	ev, _ = Logfmt(`msg=m zip="01234"`, optTest)
	if v, _ := ev.Fields.Get("zip"); v != "01234" {
		t.Fatalf("zip = %#v", v)
	}
}

func TestLogfmtLiftsTimestampAndPid(t *testing.T) {
	ev, ok := Logfmt(`ts=2026-07-12T19:00:00+09:00 level=warning msg=slow pid=99 host=db1`, optTest)
	if !ok {
		t.Fatal("parse failed")
	}
	if ev.TS != "2026-07-12T10:00:00Z" {
		t.Fatalf("ts = %q", ev.TS)
	}
	if ev.Level != "warn" || ev.PID != 99 || ev.Host != "db1" {
		t.Fatalf("envelope = %+v", ev)
	}
	if _, present := ev.Fields.Get("ts"); present {
		t.Fatal("lifted keys must leave fields")
	}
}

func TestLogfmtUnknownLevelPreservedRaw(t *testing.T) {
	// tier-honesty: an unmappable level is never guessed, only preserved.
	ev, _ := Logfmt(`level=loud msg=hi`, optTest)
	if ev.Level != "" {
		t.Fatalf("level = %q, want empty", ev.Level)
	}
	if v, _ := ev.Fields.Get("level_raw"); v != "loud" {
		t.Fatalf("level_raw = %#v", v)
	}
}

func TestLogfmtFirstAliasWinsOthersStay(t *testing.T) {
	// "ts" outranks "time"; the loser stays in fields untouched.
	ev, _ := Logfmt(`ts=1783850400 time=ignored msg=m`, optTest)
	if ev.TS != "2026-07-12T10:00:00Z" {
		t.Fatalf("ts = %q", ev.TS)
	}
	if v, _ := ev.Fields.Get("time"); v != "ignored" {
		t.Fatalf("time = %#v", v)
	}
}

func TestLogfmtRejectsProse(t *testing.T) {
	// Free text with a stray '=' must not be classified as logfmt.
	for _, line := range []string{
		"error = disk full",             // bare words around the pair
		"just a plain sentence",         // no '=' at all
		`msg="unterminated`,             // unterminated quote
		`msg="v"junk`,                   // garbage glued to closing quote
		`="empty key"`,                  // empty key
		"PANIC: something bad happened", // classic crash line
	} {
		if _, ok := Logfmt(line, optTest); ok {
			t.Fatalf("Logfmt(%q) unexpectedly succeeded", line)
		}
	}
}

func TestLogfmtUnicodeEscapeInQuotedValue(t *testing.T) {
	ev, ok := Logfmt(`msg="caf\u00e9 open"`, optTest)
	if !ok || ev.Msg != "caf\u00e9 open" {
		t.Fatalf("msg = %q, ok = %v", ev.Msg, ok)
	}
}

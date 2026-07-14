// Tests for timestamp normalization: every dialect logvert claims to read
// must land on the same canonical RFC 3339 UTC string.
package tsnorm

import (
	"testing"
	"time"
)

// optUTC pins the assumptions so tests never depend on the wall clock.
var optUTC = Options{Loc: time.UTC, Year: 2026}

func mustParse(t *testing.T, s string, opt Options) string {
	t.Helper()
	tm, ok := Parse(s, opt)
	if !ok {
		t.Fatalf("Parse(%q) failed", s)
	}
	return Format(tm)
}

func TestParseRFC3339UTC(t *testing.T) {
	if got := mustParse(t, "2026-07-12T10:00:00Z", optUTC); got != "2026-07-12T10:00:00Z" {
		t.Fatalf("got %q", got)
	}
	// Fractional seconds must survive normalization untouched.
	if got := mustParse(t, "2026-07-12T10:00:00.123456Z", optUTC); got != "2026-07-12T10:00:00.123456Z" {
		t.Fatalf("got %q", got)
	}
}

func TestParseRFC3339OffsetIsConvertedToUTC(t *testing.T) {
	// +09:00 must be folded into UTC, not preserved.
	if got := mustParse(t, "2026-07-12T19:30:00+09:00", optUTC); got != "2026-07-12T10:30:00Z" {
		t.Fatalf("got %q", got)
	}
}

func TestParseSpaceSeparatedNaiveUsesAssumedZone(t *testing.T) {
	tokyo := time.FixedZone("+09:00", 9*3600)
	got := mustParse(t, "2026-07-12 19:00:00", Options{Loc: tokyo, Year: 2026})
	if got != "2026-07-12T10:00:00Z" {
		t.Fatalf("got %q", got)
	}
}

func TestParseCLFDate(t *testing.T) {
	// The Apache/nginx access-log shape, with its offset applied.
	if got := mustParse(t, "12/Jul/2026:10:00:04 -0700", optUTC); got != "2026-07-12T17:00:04Z" {
		t.Fatalf("got %q", got)
	}
}

func TestParseBSDSyslogUsesAssumedYear(t *testing.T) {
	if got := mustParse(t, "Jul 12 10:00:03", optUTC); got != "2026-07-12T10:00:03Z" {
		t.Fatalf("got %q", got)
	}
}

func TestParseBSDSyslogSingleDigitDay(t *testing.T) {
	// RFC 3164 pads single-digit days with a space: "Jul  2".
	if got := mustParse(t, "Jul  2 08:05:00", optUTC); got != "2026-07-02T08:05:00Z" {
		t.Fatalf("got %q", got)
	}
}

func TestParseErrorLogDates(t *testing.T) {
	// nginx error-log shape (zone-less, uses the assumed zone).
	if got := mustParse(t, "2026/07/12 10:00:05", optUTC); got != "2026-07-12T10:00:05Z" {
		t.Fatalf("got %q", got)
	}
	// Apache 2.4 error-log shape with microseconds.
	got := mustParse(t, "Sun Jul 12 10:00:06.123456 2026", optUTC)
	if got != "2026-07-12T10:00:06.123456Z" {
		t.Fatalf("got %q", got)
	}
}

func TestParseEpochUnits(t *testing.T) {
	// The same instant in seconds, millis, micros, and nanos must agree.
	want := "2026-07-12T10:00:00Z"
	for _, s := range []string{"1783850400", "1783850400000", "1783850400000000", "1783850400000000000"} {
		if got := mustParse(t, s, optUTC); got != want {
			t.Fatalf("Parse(%q) = %q, want %q", s, got, want)
		}
	}
}

func TestParseEpochWithFraction(t *testing.T) {
	if got := mustParse(t, "1783850400.25", optUTC); got != "2026-07-12T10:00:00.25Z" {
		t.Fatalf("got %q", got)
	}
}

func TestParseRejectsSmallIntegersAndProse(t *testing.T) {
	// A status code or counter must never be mistaken for an epoch.
	for _, s := range []string{"200", "8080", "-1783850400", "hello", "", "12:00:00"} {
		if _, ok := Parse(s, optUTC); ok {
			t.Fatalf("Parse(%q) unexpectedly succeeded", s)
		}
	}
}

func TestFormatTrimsTrailingFractionZeros(t *testing.T) {
	tm := time.Date(2026, 7, 12, 10, 0, 0, 250_000_000, time.UTC)
	if got := Format(tm); got != "2026-07-12T10:00:00.25Z" {
		t.Fatalf("got %q", got)
	}
}

func TestParseOffsetVariants(t *testing.T) {
	for _, s := range []string{"UTC", "utc", "", "Z"} {
		loc, err := ParseOffset(s)
		if err != nil || loc != time.UTC {
			t.Fatalf("ParseOffset(%q) = %v, %v", s, loc, err)
		}
	}
	loc, err := ParseOffset("+09:00")
	if err != nil {
		t.Fatal(err)
	}
	if _, off := time.Now().In(loc).Zone(); off != 9*3600 {
		t.Fatalf("+09:00 gave offset %d", off)
	}
	loc, err = ParseOffset("-0730")
	if err != nil {
		t.Fatal(err)
	}
	if _, off := time.Now().In(loc).Zone(); off != -(7*3600 + 30*60) {
		t.Fatalf("-0730 gave offset %d", off)
	}
}

func TestParseOffsetRejectsNamedZonesAndJunk(t *testing.T) {
	// Named zones need a tz database — not offline-deterministic.
	for _, s := range []string{"Asia/Tokyo", "JST", "+25:00", "+09:99", "+9x"} {
		if _, err := ParseOffset(s); err == nil {
			t.Fatalf("ParseOffset(%q) unexpectedly succeeded", s)
		}
	}
}

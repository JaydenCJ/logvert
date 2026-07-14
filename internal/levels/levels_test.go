// Tests for severity normalization onto the six-value scale.
package levels

import "testing"

func TestNormalizeCommonSpellings(t *testing.T) {
	cases := map[string]string{
		"INFO": Info, "warning": Warn, "Err": Error, "CRIT": Fatal,
		"notice": Info, "verbose": Debug, "panic": Fatal, "trace": Trace,
		" error ": Error, // stray whitespace from sloppy producers
	}
	for in, want := range cases {
		got, ok := Normalize(in)
		if !ok || got != want {
			t.Fatalf("Normalize(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

func TestNormalizeRejectsUnknownSpellings(t *testing.T) {
	// Unknown levels must not be guessed; callers preserve them raw.
	for _, in := range []string{"", "loud", "5", "ok"} {
		if _, ok := Normalize(in); ok {
			t.Fatalf("Normalize(%q) unexpectedly succeeded", in)
		}
	}
}

func TestFromSyslogSeverityFullTable(t *testing.T) {
	want := []string{Fatal, Fatal, Fatal, Error, Warn, Info, Info, Debug}
	for sev, w := range want {
		got, ok := FromSyslogSeverity(sev)
		if !ok || got != w {
			t.Fatalf("severity %d = %q, want %q", sev, got, w)
		}
	}
	if _, ok := FromSyslogSeverity(8); ok {
		t.Fatal("severity 8 must be rejected")
	}
}

func TestFromNumericPinoScale(t *testing.T) {
	cases := map[int]string{10: Trace, 20: Debug, 30: Info, 40: Warn, 50: Error, 60: Fatal, 25: Info}
	for in, want := range cases {
		got, ok := FromNumeric(in)
		if !ok || got != want {
			t.Fatalf("FromNumeric(%d) = %q, want %q", in, got, want)
		}
	}
	for _, in := range []int{0, -5, 61, 100} {
		if _, ok := FromNumeric(in); ok {
			t.Fatalf("FromNumeric(%d) unexpectedly succeeded", in)
		}
	}
}

func TestFromHTTPStatusBands(t *testing.T) {
	cases := map[int]string{200: Info, 301: Info, 404: Warn, 499: Warn, 500: Error, 503: Error}
	for in, want := range cases {
		if got := FromHTTPStatus(in); got != want {
			t.Fatalf("FromHTTPStatus(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestFacilityNames(t *testing.T) {
	for code, want := range map[int]string{0: "kern", 4: "auth", 23: "local7"} {
		got, ok := FacilityName(code)
		if !ok || got != want {
			t.Fatalf("FacilityName(%d) = %q, want %q", code, got, want)
		}
	}
	if _, ok := FacilityName(24); ok {
		t.Fatal("facility 24 must be rejected")
	}
}

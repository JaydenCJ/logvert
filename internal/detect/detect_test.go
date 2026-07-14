// Tests for auto-detection: one line of each dialect must land on its own
// parser, ambiguous prose must fall back to raw, and forced formats must
// bypass detection entirely.
package detect

import (
	"testing"
	"time"

	"github.com/JaydenCJ/logvert/internal/event"
	"github.com/JaydenCJ/logvert/internal/parse"
	"github.com/JaydenCJ/logvert/internal/tsnorm"
)

var optTest = parse.Options{TS: tsnorm.Options{Loc: time.UTC, Year: 2026}}

func TestLineAutoDetectsEveryFormat(t *testing.T) {
	cases := map[string]string{
		`{"level":"info","msg":"up"}`:                                          event.SourceJSON,
		`level=info msg=up`:                                                    event.SourceLogfmt,
		`<34>1 2026-07-12T10:00:00Z h app - - - m`:                             event.SourceSyslog,
		`Jul 12 10:00:03 web1 sshd[811]: ok`:                                   event.SourceSyslog,
		`127.0.0.1 - - [12/Jul/2026:10:00:04 +0000] "GET / HTTP/1.1" 200 5`:    event.SourceAccess,
		`2026/07/12 10:00:05 [error] 1#0: boom`:                                event.SourceNginxError,
		`[Sun Jul 12 10:00:06 2026] [core:error] [pid 9] AH00126: bad request`: event.SourceApacheError,
	}
	for line, want := range cases {
		ev := Line(line, "auto", optTest)
		if ev.Source != want {
			t.Fatalf("Line(%q) source = %q, want %q", line, ev.Source, want)
		}
	}
}

func TestLineUnmatchedFallsBackToRawWithFullLine(t *testing.T) {
	line := "PANIC: worker crashed with code 3"
	ev := Line(line, "auto", optTest)
	if ev.Source != event.SourceRaw || ev.Msg != line {
		t.Fatalf("got %+v", ev)
	}
}

func TestLineForcedFormatSkipsDetection(t *testing.T) {
	// A perfectly valid JSON line forced through --format raw stays raw.
	line := `{"level":"info","msg":"up"}`
	ev := Line(line, event.SourceRaw, optTest)
	if ev.Source != event.SourceRaw || ev.Msg != line {
		t.Fatalf("got %+v", ev)
	}
	// Forcing logfmt on a JSON line must not half-parse; it goes raw.
	ev = Line(line, event.SourceLogfmt, optTest)
	if ev.Source != event.SourceRaw {
		t.Fatalf("source = %q", ev.Source)
	}
}

func TestValidFormatTable(t *testing.T) {
	for _, name := range append([]string{"auto", "raw"}, Formats...) {
		if !ValidFormat(name) {
			t.Fatalf("ValidFormat(%q) = false", name)
		}
	}
	for _, name := range []string{"", "yaml", "JSON", "Auto"} {
		if ValidFormat(name) {
			t.Fatalf("ValidFormat(%q) = true", name)
		}
	}
}

func TestLineDetectionIsDeterministic(t *testing.T) {
	// The same ambiguous-ish line must classify identically every time —
	// map iteration or similar nondeterminism would corrupt pipelines.
	line := `Jul 12 10:00:03 web1 app: status=200 ok`
	first := Line(line, "auto", optTest)
	for i := 0; i < 100; i++ {
		if got := Line(line, "auto", optTest); got.Source != first.Source {
			t.Fatalf("iteration %d: %q != %q", i, got.Source, first.Source)
		}
	}
}

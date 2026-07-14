// In-process CLI tests: Run is exercised with fake streams, covering flag
// handling, exit codes, mixed-stream conversion, --map lifting, and stats.
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JaydenCJ/logvert/internal/version"
)

// run executes the CLI in-process and captures both streams.
func run(t *testing.T, stdin string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb strings.Builder
	code = Run(args, strings.NewReader(stdin), &out, &errb)
	return code, out.String(), errb.String()
}

// pinned makes every test independent of the wall clock and host zone.
var pinned = []string{"--assume-year", "2026", "--assume-tz", "UTC"}

func with(extra ...string) []string { return append(append([]string{}, pinned...), extra...) }

func TestVersionSubcommandAndFlag(t *testing.T) {
	for _, argv := range [][]string{{"version"}, {"--version"}, {"-v"}} {
		code, out, _ := run(t, "", argv...)
		if code != ExitOK || out != "logvert "+version.Version+"\n" {
			t.Fatalf("argv %v: code %d, out %q", argv, code, out)
		}
	}
}

func TestHelpMentionsEveryFlag(t *testing.T) {
	code, out, _ := run(t, "", "--help")
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
	for _, want := range []string{"--format", "--flat", "--strict", "--drop-raw",
		"--map", "--drop-field", "--assume-tz", "--assume-year", "--stats"} {
		if !strings.Contains(out, want) {
			t.Fatalf("help is missing %s", want)
		}
	}
	// The help must also name every supported input format.
	for _, want := range []string{"logfmt", "syslog", "nginx-error", "apache-error", "access", "json", "raw"} {
		if !strings.Contains(out, want) {
			t.Fatalf("help must list format %q", want)
		}
	}
}

func TestMixedStreamConvertsEachLineByItsOwnFormat(t *testing.T) {
	stdin := `level=info msg=one
{"level":"warn","msg":"two"}
Jul 12 10:00:03 web1 app[5]: three
`
	code, out, _ := run(t, stdin, with()...)
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 output lines, got %d: %q", len(lines), out)
	}
	for i, want := range []string{`"source":"logfmt"`, `"source":"json"`, `"source":"syslog"`} {
		if !strings.Contains(lines[i], want) {
			t.Fatalf("line %d = %s, want %s", i, lines[i], want)
		}
	}
}

func TestOutputIsByteIdenticalAcrossRuns(t *testing.T) {
	stdin := `ts=1783850400 level=info msg=steady count=7
{"time":"2026-07-12T10:00:01Z","level":"error","msg":"x","ctx":{"b":1,"a":2}}
`
	_, first, _ := run(t, stdin, with()...)
	for i := 0; i < 5; i++ {
		_, again, _ := run(t, stdin, with()...)
		if again != first {
			t.Fatalf("run %d differs:\n%s\n%s", i, first, again)
		}
	}
}

func TestBlankAndCRLFLinesAreHandled(t *testing.T) {
	code, out, _ := run(t, "\n  \nlevel=info msg=hi\n\n", with()...)
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
	if n := strings.Count(out, "\n"); n != 1 {
		t.Fatalf("want 1 output line, got %d", n)
	}
	// Windows-produced logs must not grow a trailing \r in the message.
	code, out, _ = run(t, "level=info msg=hi\r\n", with()...)
	if code != ExitOK || strings.Contains(out, `\r`) || !strings.Contains(out, `"msg":"hi"`) {
		t.Fatalf("code %d out %q", code, out)
	}
}

func TestStrictExitsOneOnRawLines(t *testing.T) {
	code, out, errb := run(t, "total gibberish here\n", with("--strict")...)
	if code != ExitStrict {
		t.Fatalf("code %d, want %d", code, ExitStrict)
	}
	// The raw event is still emitted; strict only changes the exit code.
	if !strings.Contains(out, `"source":"raw"`) {
		t.Fatalf("out %q", out)
	}
	if !strings.Contains(errb, "1 line failed to parse") {
		t.Fatalf("stderr %q", errb)
	}
	// Two raw lines must pluralize.
	_, _, errb = run(t, "gibberish one\ngibberish two\n", with("--strict")...)
	if !strings.Contains(errb, "2 lines failed to parse") {
		t.Fatalf("stderr %q", errb)
	}
}

func TestDropRawSuppressesUnparseableLines(t *testing.T) {
	code, out, _ := run(t, "gibberish\nlevel=info msg=keep\n", with("--drop-raw")...)
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
	if strings.Contains(out, "gibberish") || !strings.Contains(out, `"msg":"keep"`) {
		t.Fatalf("out %q", out)
	}
}

func TestForcedFormatAppliesToEveryLine(t *testing.T) {
	// Under --format raw even valid JSON stays a raw event.
	code, out, _ := run(t, `{"msg":"x"}`+"\n", with("--format", "raw")...)
	if code != ExitOK || !strings.Contains(out, `"source":"raw"`) {
		t.Fatalf("code %d out %q", code, out)
	}
}

func TestMapRenamesAndLiftsCanonical(t *testing.T) {
	// severity_text→level and svc→app lift; latency→duration_ms renames.
	stdin := `{"severity_text":"WARN","svc":"pay","latency":12,"msg":"slow"}` + "\n"
	code, out, _ := run(t, stdin, with("--map", "severity_text=level", "--map", "svc=app", "--map", "latency=duration_ms")...)
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
	want := `{"level":"warn","msg":"slow","app":"pay","source":"json","fields":{"duration_ms":12}}`
	if strings.TrimSpace(out) != want {
		t.Fatalf("got  %s\nwant %s", strings.TrimSpace(out), want)
	}
}

func TestMapToSourceIsRejected(t *testing.T) {
	code, _, errb := run(t, "", with("--map", "x=source")...)
	if code != ExitUsage || !strings.Contains(errb, "source") {
		t.Fatalf("code %d stderr %q", code, errb)
	}
}

func TestDropFieldRemovesNoise(t *testing.T) {
	stdin := `127.0.0.1 - - [12/Jul/2026:10:00:04 +0000] "GET / HTTP/1.1" 200 5 "-" "curl/8"` + "\n"
	code, out, _ := run(t, stdin, with("--drop-field", "user_agent", "--drop-field", "remote_addr")...)
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
	if strings.Contains(out, "user_agent") || strings.Contains(out, "remote_addr") {
		t.Fatalf("out %q", out)
	}
}

func TestFlatModeMergesFields(t *testing.T) {
	code, out, _ := run(t, "level=info msg=m status=200\n", with("--flat")...)
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
	want := `{"level":"info","msg":"m","source":"logfmt","status":200}`
	if strings.TrimSpace(out) != want {
		t.Fatalf("got %s", strings.TrimSpace(out))
	}
}

func TestStatsSummaryOnStderr(t *testing.T) {
	stdin := "level=info msg=a\nlevel=info msg=b\ngibberish\n\n"
	code, out, errb := run(t, stdin, with("--stats")...)
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
	if strings.Contains(out, "logvert:") {
		t.Fatal("stats must go to stderr, not stdout")
	}
	want := "logvert: 3 lines in — logfmt 2, raw 1 (1 blank line skipped)\n"
	if errb != want {
		t.Fatalf("stderr %q, want %q", errb, want)
	}
	// A single line must not read "1 lines".
	_, _, errb = run(t, "level=info msg=a\n", with("--stats")...)
	if want := "logvert: 1 line in — logfmt 1\n"; errb != want {
		t.Fatalf("stderr %q, want %q", errb, want)
	}
}

func TestUsageErrorsExitTwo(t *testing.T) {
	for _, argv := range [][]string{
		{"--format", "yaml"},
		{"--assume-tz", "Mars/Olympus"},
		{"--map", "broken"},
		{"--max-line-bytes", "10"},
		{"--no-such-flag"},
	} {
		code, _, errb := run(t, "", argv...)
		if code != ExitUsage {
			t.Fatalf("argv %v: code %d, want %d (stderr %q)", argv, code, ExitUsage, errb)
		}
	}
}

func TestMaxLineBytesIsEnforced(t *testing.T) {
	// Regression: bufio.Scanner takes the larger of the initial buffer
	// capacity and max, so a limit below 64 KiB used to be ignored.
	long := strings.Repeat("x", 2048) + "\n"
	code, _, errb := run(t, long, with("--max-line-bytes", "1024")...)
	// The error must name the flag to raise, not leak bufio internals.
	if code != ExitRuntime || !strings.Contains(errb, "--max-line-bytes (1024)") {
		t.Fatalf("overlong line: code %d stderr %q, want %d", code, errb, ExitRuntime)
	}
	// A line within the limit still converts normally.
	code, out, _ := run(t, "level=info msg=fits\n", with("--max-line-bytes", "1024")...)
	if code != ExitOK || !strings.Contains(out, `"msg":"fits"`) {
		t.Fatalf("within limit: code %d out %q", code, out)
	}
}

func TestReadsFilesInArgumentOrder(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.log")
	b := filepath.Join(dir, "b.log")
	if err := os.WriteFile(a, []byte("msg=first level=info\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte(`{"msg":"second"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := run(t, "", with(a, b)...)
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(out, "first") || strings.Index(out, "first") > strings.Index(out, "second") {
		t.Fatalf("out %q", out)
	}
	// A missing file is an I/O failure, not a usage error.
	code, _, errb := run(t, "", with(filepath.Join(dir, "absent.log"))...)
	if code != ExitRuntime || errb == "" {
		t.Fatalf("code %d stderr %q", code, errb)
	}
}

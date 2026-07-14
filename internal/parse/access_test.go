// Tests for HTTP server logs: Common/Combined access lines and the
// Apache/nginx error-log dialects, including status→level derivation and
// nginx context-pair promotion.
package parse

import (
	"encoding/json"
	"testing"
)

func TestAccessCommonLogFormat(t *testing.T) {
	line := `127.0.0.1 - frank [10/Oct/2000:13:55:36 -0700] "GET /apache_pb.gif HTTP/1.0" 200 2326`
	ev, ok := Access(line, optTest)
	if !ok {
		t.Fatal("parse failed")
	}
	if ev.TS != "2000-10-10T20:55:36Z" {
		t.Fatalf("ts = %q", ev.TS)
	}
	if ev.Level != "info" || ev.Msg != "GET /apache_pb.gif HTTP/1.0" {
		t.Fatalf("envelope = %+v", ev)
	}
	for k, want := range map[string]any{
		"remote_addr": "127.0.0.1", "user": "frank", "method": "GET",
		"path": "/apache_pb.gif", "proto": "HTTP/1.0",
		"status": json.Number("200"), "bytes": json.Number("2326"),
	} {
		if v, _ := ev.Fields.Get(k); v != want {
			t.Fatalf("%s = %#v, want %#v", k, v, want)
		}
	}
}

func TestAccessCombinedAddsRefererAndUserAgent(t *testing.T) {
	line := `127.0.0.1 - - [12/Jul/2026:10:00:04 +0000] "POST /login HTTP/1.1" 302 0 "http://example.test/" "Mozilla/5.0 (X11; Linux)"`
	ev, ok := Access(line, optTest)
	if !ok {
		t.Fatal("parse failed")
	}
	if v, _ := ev.Fields.Get("referer"); v != "http://example.test/" {
		t.Fatalf("referer = %#v", v)
	}
	if v, _ := ev.Fields.Get("user_agent"); v != "Mozilla/5.0 (X11; Linux)" {
		t.Fatalf("user_agent = %#v", v)
	}
	if _, present := ev.Fields.Get("user"); present {
		t.Fatal(`"-" user must be omitted`)
	}
	// "-" bytes (e.g. on 304 responses) must be omitted, not zeroed.
	ev, ok = Access(`127.0.0.1 - - [12/Jul/2026:10:00:04 +0000] "GET / HTTP/1.1" 304 -`, optTest)
	if !ok {
		t.Fatal("parse failed")
	}
	if _, present := ev.Fields.Get("bytes"); present {
		t.Fatal(`"-" bytes must be omitted`)
	}
}

func TestAccessStatusDrivesLevel(t *testing.T) {
	// 5xx→error and 4xx→warn make access logs alertable downstream.
	for status, want := range map[string]string{"200": "info", "404": "warn", "503": "error"} {
		line := `127.0.0.1 - - [12/Jul/2026:10:00:04 +0000] "GET / HTTP/1.1" ` + status + ` 5`
		ev, ok := Access(line, optTest)
		if !ok || ev.Level != want {
			t.Fatalf("status %s → level %q, want %q", status, ev.Level, want)
		}
	}
}

func TestAccessMalformedRequestStaysMessageOnly(t *testing.T) {
	// Port scanners produce junk request lines; keep them, unsplit.
	line := `127.0.0.1 - - [12/Jul/2026:10:00:04 +0000] "\x16\x03\x01" 400 0`
	ev, ok := Access(line, optTest)
	if !ok {
		t.Fatal("parse failed")
	}
	if _, present := ev.Fields.Get("method"); present {
		t.Fatal("junk request must not yield a method")
	}
	if ev.Level != "warn" {
		t.Fatalf("level = %q", ev.Level)
	}
}

func TestAccessEscapedQuoteInUserAgent(t *testing.T) {
	// nginx escape=default writes \" inside quoted strings.
	line := `127.0.0.1 - - [12/Jul/2026:10:00:04 +0000] "GET / HTTP/1.1" 200 5 "-" "agent \"X\""`
	ev, ok := Access(line, optTest)
	if !ok {
		t.Fatal("parse failed")
	}
	if v, _ := ev.Fields.Get("user_agent"); v != `agent "X"` {
		t.Fatalf("user_agent = %#v", v)
	}
}

func TestAccessRejectsNonAccessLines(t *testing.T) {
	for _, line := range []string{
		`level=info msg=hi`, // logfmt
		`127.0.0.1 - - [12/Jul/2026:10:00:04 +0000] "GET / HTTP/1.1" 999 5`,   // impossible status
		`127.0.0.1 - - [12/Jul/2026:10:00:04 +0000] "GET / HTTP/1.1"`,         // truncated
		`127.0.0.1 - - not-a-date "GET / HTTP/1.1" 200 5`,                     // no bracketed date
		`127.0.0.1 - - [12/Jul/2026:10:00:04 +0000] "GET / HTTP/1.1" 200 5 x`, // 8 tokens, unquoted tail
	} {
		if _, ok := Access(line, optTest); ok {
			t.Fatalf("Access(%q) unexpectedly succeeded", line)
		}
	}
}

func TestNginxErrorFullLine(t *testing.T) {
	line := `2026/07/12 10:00:05 [error] 4242#4243: *17 connect() failed (111: Connection refused) while connecting to upstream, client: 127.0.0.1, server: example.test, request: "GET /health HTTP/1.1"`
	ev, ok := NginxError(line, optTest)
	if !ok {
		t.Fatal("parse failed")
	}
	if ev.TS != "2026-07-12T10:00:05Z" || ev.Level != "error" || ev.PID != 4242 {
		t.Fatalf("envelope = %+v", ev)
	}
	if ev.Msg != "connect() failed (111: Connection refused) while connecting to upstream" {
		t.Fatalf("msg = %q", ev.Msg)
	}
	for k, want := range map[string]any{
		"tid": json.Number("4243"), "connection": json.Number("17"),
		"client": "127.0.0.1", "server": "example.test",
		"request": "GET /health HTTP/1.1",
	} {
		if v, _ := ev.Fields.Get(k); v != want {
			t.Fatalf("%s = %#v, want %#v", k, v, want)
		}
	}
}

func TestNginxErrorWithoutContextPairs(t *testing.T) {
	ev, ok := NginxError(`2026/07/12 10:00:05 [warn] 7#0: low on workers`, optTest)
	if !ok {
		t.Fatal("parse failed")
	}
	if ev.Level != "warn" || ev.PID != 7 || ev.Msg != "low on workers" {
		t.Fatalf("envelope = %+v", ev)
	}
	if _, present := ev.Fields.Get("tid"); present {
		t.Fatal("tid 0 must be omitted")
	}
}

func TestNginxErrorCommaInsideQuotedRequestIsSafe(t *testing.T) {
	// A comma inside the quoted request must not break context peeling.
	line := `2026/07/12 10:00:05 [error] 1#0: *2 boom, client: 127.0.0.1, request: "GET /a,b?x=1, HTTP/1.1"`
	ev, ok := NginxError(line, optTest)
	if !ok {
		t.Fatal("parse failed")
	}
	if v, _ := ev.Fields.Get("request"); v != "GET /a,b?x=1, HTTP/1.1" {
		t.Fatalf("request = %#v", v)
	}
	if ev.Msg != "boom" {
		t.Fatalf("msg = %q", ev.Msg)
	}
}

func TestApacheError24FullLine(t *testing.T) {
	line := `[Sun Jul 12 10:00:06.123456 2026] [proxy:error] [pid 951:tid 140] [client 127.0.0.1:52044] AH00898: Error reading from remote server`
	ev, ok := ApacheError(line, optTest)
	if !ok {
		t.Fatal("parse failed")
	}
	if ev.TS != "2026-07-12T10:00:06.123456Z" || ev.Level != "error" || ev.PID != 951 {
		t.Fatalf("envelope = %+v", ev)
	}
	if ev.Msg != "Error reading from remote server" {
		t.Fatalf("msg = %q", ev.Msg)
	}
	for k, want := range map[string]any{
		"module": "proxy", "tid": json.Number("140"),
		"client": "127.0.0.1:52044", "code": "AH00898",
	} {
		if v, _ := ev.Fields.Get(k); v != want {
			t.Fatalf("%s = %#v, want %#v", k, v, want)
		}
	}
}

func TestApacheError22LegacyShape(t *testing.T) {
	// Apache 2.2: bare level, no module/pid blocks, no AH code.
	line := `[Fri Oct 11 22:14:15 2026] [error] [client 127.0.0.1] File does not exist: /var/www/favicon.ico`
	ev, ok := ApacheError(line, optTest)
	if !ok {
		t.Fatal("parse failed")
	}
	if ev.Level != "error" || ev.Msg != "File does not exist: /var/www/favicon.ico" {
		t.Fatalf("envelope = %+v", ev)
	}
	if _, present := ev.Fields.Get("module"); present {
		t.Fatal("2.2 shape has no module")
	}
}

func TestErrorLogsRejectNonMatching(t *testing.T) {
	for _, line := range []string{
		`2026/07/12 10:00:05 no-bracket message`,     // nginx without [level]
		`2026/07/12 10:00:05 [loud] 1#0: bad level`,  // unknown severity
		`[Sun Jul 12 10:00:06 2026] no second block`, // apache without level block
		`[not a date] [error] msg`,                   // apache with junk date
		`plain text`,                                 //
	} {
		if _, ok := NginxError(line, optTest); ok {
			t.Fatalf("NginxError(%q) unexpectedly succeeded", line)
		}
		if _, ok := ApacheError(line, optTest); ok {
			t.Fatalf("ApacheError(%q) unexpectedly succeeded", line)
		}
	}
}

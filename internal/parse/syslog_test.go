// Tests for both syslog generations: RFC 5424 headers and structured
// data, RFC 3164 / BSD tags, and PRI decoding into level + facility.
package parse

import (
	"testing"

	"github.com/JaydenCJ/logvert/internal/event"
)

func TestSyslog5424FullLine(t *testing.T) {
	line := `<165>1 2026-07-12T10:00:02.003Z web1 evtlog 1024 ID47 [origin ip="127.0.0.1" sw="app"] service started`
	ev, ok := Syslog(line, optTest)
	if !ok {
		t.Fatal("parse failed")
	}
	if ev.TS != "2026-07-12T10:00:02.003Z" || ev.Host != "web1" || ev.App != "evtlog" || ev.PID != 1024 {
		t.Fatalf("envelope = %+v", ev)
	}
	if ev.Msg != "service started" {
		t.Fatalf("msg = %q", ev.Msg)
	}
	// PRI 165 = facility 20 (local4), severity 5 (notice → info).
	if ev.Level != "info" {
		t.Fatalf("level = %q", ev.Level)
	}
	if v, _ := ev.Fields.Get("facility"); v != "local4" {
		t.Fatalf("facility = %#v", v)
	}
	if v, _ := ev.Fields.Get("origin.ip"); v != "127.0.0.1" {
		t.Fatalf("origin.ip = %#v", v)
	}
	if v, _ := ev.Fields.Get("msgid"); v != "ID47" {
		t.Fatalf("msgid = %#v", v)
	}
}

func TestSyslog5424NilFieldsAndNoMessage(t *testing.T) {
	ev, ok := Syslog(`<34>1 - - - - -`, optTest)
	if !ok {
		t.Fatal("parse failed")
	}
	if ev.TS != "" || ev.Host != "" || ev.App != "" || ev.PID != 0 || ev.Msg != "" {
		t.Fatalf("nil fields must stay absent: %+v", ev)
	}
	// PRI 34 = facility 4 (auth), severity 2 (crit → fatal).
	if ev.Level != "fatal" {
		t.Fatalf("level = %q", ev.Level)
	}
}

func TestSyslog5424SDValueEscapes(t *testing.T) {
	line := `<13>1 2026-07-12T10:00:00Z h app - - [x q="say \"hi\" \] done"] m`
	ev, ok := Syslog(line, optTest)
	if !ok {
		t.Fatal("parse failed")
	}
	if v, _ := ev.Fields.Get("x.q"); v != `say "hi" ] done` {
		t.Fatalf("x.q = %#v", v)
	}
}

func TestSyslog5424MultipleSDBlocksCannotCollide(t *testing.T) {
	// Params flatten as sd-id.param, so two blocks with the same param
	// name stay distinct.
	line := `<13>1 2026-07-12T10:00:00Z h app - - [a id="1"][b id="2"] m`
	ev, ok := Syslog(line, optTest)
	if !ok {
		t.Fatal("parse failed")
	}
	va, _ := ev.Fields.Get("a.id")
	vb, _ := ev.Fields.Get("b.id")
	if va != "1" || vb != "2" {
		t.Fatalf("a.id = %#v, b.id = %#v", va, vb)
	}
}

func TestSyslog5424NonNumericProcIDStaysField(t *testing.T) {
	// systemd writes non-numeric PROCIDs; they must not be forced to int.
	ev, ok := Syslog(`<13>1 2026-07-12T10:00:00Z h app worker-3 - - m`, optTest)
	if !ok {
		t.Fatal("parse failed")
	}
	if ev.PID != 0 {
		t.Fatalf("pid = %d", ev.PID)
	}
	if v, _ := ev.Fields.Get("procid"); v != "worker-3" {
		t.Fatalf("procid = %#v", v)
	}
}

func TestSyslog3164WithPriTagAndPid(t *testing.T) {
	ev, ok := Syslog(`<86>Jul 12 10:00:03 web1 sshd[811]: session opened`, optTest)
	if !ok {
		t.Fatal("parse failed")
	}
	if ev.TS != "2026-07-12T10:00:03Z" {
		t.Fatalf("ts = %q (assume-year must apply)", ev.TS)
	}
	if ev.Host != "web1" || ev.App != "sshd" || ev.PID != 811 {
		t.Fatalf("envelope = %+v", ev)
	}
	if ev.Msg != "session opened" {
		t.Fatalf("msg = %q", ev.Msg)
	}
	// PRI 86 = facility 10 (authpriv), severity 6 (info).
	if ev.Level != "info" {
		t.Fatalf("level = %q", ev.Level)
	}
	if v, _ := ev.Fields.Get("facility"); v != "authpriv" {
		t.Fatalf("facility = %#v", v)
	}
}

func TestSyslog3164WithoutPri(t *testing.T) {
	// Plain /var/log/syslog shape: no <PRI>, so no level or facility.
	ev, ok := Syslog(`Jul  2 08:05:00 db1 cron[42]: job done`, optTest)
	if !ok {
		t.Fatal("parse failed")
	}
	if ev.Level != "" {
		t.Fatalf("level = %q, want empty", ev.Level)
	}
	if ev.TS != "2026-07-02T08:05:00Z" || ev.App != "cron" || ev.PID != 42 {
		t.Fatalf("envelope = %+v", ev)
	}
	if _, present := ev.Fields.Get("facility"); present {
		t.Fatal("no PRI means no facility field")
	}
}

func TestSyslog3164TagWithoutPid(t *testing.T) {
	ev, ok := Syslog(`Jul 12 10:00:03 web1 kernel: oom-killer invoked`, optTest)
	if !ok {
		t.Fatal("parse failed")
	}
	if ev.App != "kernel" || ev.PID != 0 || ev.Msg != "oom-killer invoked" {
		t.Fatalf("envelope = %+v", ev)
	}
	if ev.Source != event.SourceSyslog {
		t.Fatalf("source = %q", ev.Source)
	}
}

func TestSyslog3164ContentWithoutTagKeepsWholeMessage(t *testing.T) {
	// "last message repeated" style content has no TAG.
	ev, ok := Syslog(`Jul 12 10:00:03 web1 last message repeated 2 times`, optTest)
	if !ok {
		t.Fatal("parse failed")
	}
	if ev.App != "" || ev.Msg != "last message repeated 2 times" {
		t.Fatalf("envelope = %+v", ev)
	}
}

func TestSyslogRejectsNonSyslog(t *testing.T) {
	for _, line := range []string{
		"hello world",
		`<999>1 2026-07-12T10:00:00Z h a - - - m`, // PRI out of range
		"<34>",                       // PRI with nothing after
		"Jul 12 10:00 web1 app: msg", // truncated timestamp
	} {
		if _, ok := Syslog(line, optTest); ok {
			t.Fatalf("Syslog(%q) unexpectedly succeeded", line)
		}
	}
}

func TestSyslog5424BOMStripped(t *testing.T) {
	ev, ok := Syslog("<13>1 2026-07-12T10:00:00Z h app - - - \uFEFFhello", optTest)
	if !ok || ev.Msg != "hello" {
		t.Fatalf("msg = %q, ok = %v", ev.Msg, ok)
	}
}

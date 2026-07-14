// Package levels normalizes the many severity spellings found in the wild
// onto one six-value scale: trace, debug, info, warn, error, fatal.
package levels

import "strings"

// Canonical severity values, mildest first.
const (
	Trace = "trace"
	Debug = "debug"
	Info  = "info"
	Warn  = "warn"
	Error = "error"
	Fatal = "fatal"
)

// aliases maps lowercase spellings seen in real logs to canonical values.
// Syslog's notice folds into info and its emergency/alert/critical tiers
// fold into fatal — six levels are what downstream dashboards understand.
var aliases = map[string]string{
	"trace": Trace, "finest": Trace,
	"debug": Debug, "dbg": Debug, "fine": Debug, "verbose": Debug, "finer": Debug,
	"info": Info, "informational": Info, "information": Info, "notice": Info,
	"warn": Warn, "warning": Warn,
	"error": Error, "err": Error, "severe": Error,
	"fatal": Fatal, "critical": Fatal, "crit": Fatal,
	"alert": Fatal, "emerg": Fatal, "emergency": Fatal, "panic": Fatal,
}

// Normalize maps a severity spelling to its canonical value. ok is false
// for spellings we do not recognize; callers keep the original in
// fields.level_raw so no information is lost.
func Normalize(s string) (string, bool) {
	v, ok := aliases[strings.ToLower(strings.TrimSpace(s))]
	return v, ok
}

// syslogSeverity maps RFC 5424 numeric severities (0–7) onto the scale.
var syslogSeverity = [8]string{Fatal, Fatal, Fatal, Error, Warn, Info, Info, Debug}

// FromSyslogSeverity converts a syslog severity code (PRI & 7).
func FromSyslogSeverity(sev int) (string, bool) {
	if sev < 0 || sev > 7 {
		return "", false
	}
	return syslogSeverity[sev], true
}

// FromNumeric converts the numeric levels emitted by popular structured
// loggers (10 trace … 60 fatal, the pino/bunyan convention).
func FromNumeric(n int) (string, bool) {
	switch {
	case n <= 0:
		return "", false
	case n <= 10:
		return Trace, true
	case n <= 20:
		return Debug, true
	case n <= 30:
		return Info, true
	case n <= 40:
		return Warn, true
	case n <= 50:
		return Error, true
	case n <= 60:
		return Fatal, true
	default:
		return "", false
	}
}

// FromHTTPStatus derives a severity from an HTTP status code, so access
// logs slot into level-based alerting: 5xx→error, 4xx→warn, else info.
func FromHTTPStatus(status int) string {
	switch {
	case status >= 500:
		return Error
	case status >= 400:
		return Warn
	default:
		return Info
	}
}

// Facilities maps syslog facility codes (PRI >> 3) to their names.
var Facilities = [24]string{
	"kern", "user", "mail", "daemon", "auth", "syslog", "lpr", "news",
	"uucp", "cron", "authpriv", "ftp", "ntp", "audit", "alert", "clock",
	"local0", "local1", "local2", "local3", "local4", "local5", "local6", "local7",
}

// FacilityName returns the textual name for a facility code.
func FacilityName(code int) (string, bool) {
	if code < 0 || code >= len(Facilities) {
		return "", false
	}
	return Facilities[code], true
}

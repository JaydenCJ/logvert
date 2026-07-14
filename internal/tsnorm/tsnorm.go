// Package tsnorm normalizes the timestamp dialects found in real log files
// (RFC 3339, epoch numbers, CLF, BSD syslog, Apache/nginx error dates) into
// a single canonical form: RFC 3339 in UTC.
package tsnorm

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Options carry the two assumptions needed for under-specified timestamps.
type Options struct {
	// Loc is applied to timestamps that carry no zone of their own
	// (BSD syslog, nginx error logs, bare "2026-01-02 15:04:05").
	Loc *time.Location
	// Year is applied to BSD syslog timestamps, which carry none.
	Year int
}

// Default returns UTC and the current year — the safest guesses when the
// user does not override them with --assume-tz / --assume-year.
func Default() Options {
	return Options{Loc: time.UTC, Year: time.Now().Year()}
}

func (o Options) loc() *time.Location {
	if o.Loc == nil {
		return time.UTC
	}
	return o.Loc
}

// zoned layouts carry their own offset; opt.Loc is ignored for these.
var zonedLayouts = []string{
	time.RFC3339Nano,                      // 2026-07-12T10:00:00.123Z
	"2006-01-02T15:04:05.999999999Z0700",  // zone without colon
	"2006-01-02 15:04:05.999999999Z07:00", // space separator
	"2006-01-02 15:04:05.999999999 -0700", // Go default-ish
	"02/Jan/2006:15:04:05 -0700",          // CLF (Apache/nginx access)
}

// naiveLayouts carry no zone; they are parsed in opt.Loc.
var naiveLayouts = []string{
	"2006-01-02T15:04:05.999999999",   // ISO without zone
	"2006-01-02 15:04:05.999999999",   // ISO with space, no zone
	"2006/01/02 15:04:05",             // nginx error log
	"Mon Jan _2 15:04:05.999999 2006", // Apache 2.4 error log
	"Mon Jan _2 15:04:05 2006",        // Apache 2.2 error log
}

// Parse converts a textual timestamp in any supported dialect to a
// time.Time. The boolean is false when no dialect matched.
func Parse(s string, opt Options) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if t, ok := ParseEpoch(s); ok {
		return t, true
	}
	for _, layout := range zonedLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	for _, layout := range naiveLayouts {
		if t, err := time.ParseInLocation(layout, s, opt.loc()); err == nil {
			return t, true
		}
	}
	if t, ok := parseBSD(s, opt); ok {
		return t, true
	}
	return time.Time{}, false
}

// parseBSD handles the RFC 3164 "Jan  2 15:04:05" shape, which has neither
// a year nor a zone; both come from opt.
func parseBSD(s string, opt Options) (time.Time, bool) {
	t, err := time.ParseInLocation("Jan _2 15:04:05", s, opt.loc())
	if err != nil {
		return time.Time{}, false
	}
	year := opt.Year
	if year == 0 {
		year = time.Now().Year()
	}
	return t.AddDate(year, 0, 0), true
}

// ParseEpoch converts a Unix epoch rendered as a decimal string. Magnitude
// decides the unit: seconds < 1e11 ≤ millis < 1e14 ≤ micros < 1e17 ≤ nanos,
// which covers 1973–5138 unambiguously. A fractional part refines seconds
// or milliseconds ("1720780800.25").
func ParseEpoch(s string) (time.Time, bool) {
	intPart, fracPart, hasFrac := strings.Cut(s, ".")
	neg := strings.HasPrefix(intPart, "-")
	if neg {
		// Pre-1970 epochs never appear in logs; a leading minus is far
		// more likely a stray token than a timestamp.
		return time.Time{}, false
	}
	if intPart == "" || !digitsOnly(intPart) {
		return time.Time{}, false
	}
	if hasFrac && (fracPart == "" || !digitsOnly(fracPart)) {
		return time.Time{}, false
	}
	n, err := strconv.ParseInt(intPart, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	var sec, nsec int64
	switch {
	case n < 100_000_000: // fewer than 9 digits: a counter, not a date
		return time.Time{}, false
	case n < 100_000_000_000: // seconds
		sec, nsec = n, fracNanos(fracPart, 9)
	case n < 100_000_000_000_000: // milliseconds
		sec, nsec = n/1_000, (n%1_000)*1_000_000+fracNanos(fracPart, 6)
	case n < 100_000_000_000_000_000: // microseconds
		sec, nsec = n/1_000_000, (n%1_000_000)*1_000+fracNanos(fracPart, 3)
	default: // nanoseconds
		sec, nsec = n/1_000_000_000, n%1_000_000_000
	}
	return time.Unix(sec, nsec).UTC(), true
}

// fracNanos converts a fractional-digit string to nanoseconds given how
// many digits of headroom remain below the integer unit.
func fracNanos(frac string, digits int) int64 {
	if frac == "" {
		return 0
	}
	if len(frac) > digits {
		frac = frac[:digits]
	}
	n, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return 0
	}
	for i := len(frac); i < digits; i++ {
		n *= 10
	}
	return n
}

func digitsOnly(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}

// Format renders a time as canonical RFC 3339 UTC. Sub-second digits are
// kept only when present, so "10:00:00Z" does not grow spurious zeros.
func Format(t time.Time) string {
	t = t.UTC()
	if t.Nanosecond() == 0 {
		return t.Format("2006-01-02T15:04:05Z")
	}
	return t.Format("2006-01-02T15:04:05.999999999Z07:00")
}

// ParseOffset resolves an --assume-tz value. Only "UTC" and fixed numeric
// offsets ("+09:00", "-0700", "+09") are accepted: named zones would need a
// tz database lookup, which is neither offline-safe nor deterministic.
func ParseOffset(s string) (*time.Location, error) {
	if strings.EqualFold(s, "UTC") || s == "Z" || s == "" {
		return time.UTC, nil
	}
	sign := 1
	switch s[0] {
	case '+':
	case '-':
		sign = -1
	default:
		return nil, fmt.Errorf("timezone must be UTC or a numeric offset like +09:00, got %q", s)
	}
	body := strings.ReplaceAll(s[1:], ":", "")
	if len(body) == 2 {
		body += "00"
	}
	if len(body) != 4 || !digitsOnly(body) {
		return nil, fmt.Errorf("timezone must be UTC or a numeric offset like +09:00, got %q", s)
	}
	hh, _ := strconv.Atoi(body[:2])
	mm, _ := strconv.Atoi(body[2:])
	if hh > 14 || mm > 59 {
		return nil, fmt.Errorf("timezone offset out of range: %q", s)
	}
	offset := sign * (hh*3600 + mm*60)
	return time.FixedZone(s, offset), nil
}

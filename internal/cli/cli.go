// Package cli implements the logvert command-line interface. Run takes
// argv plus explicit streams and returns an exit code, so the entire
// surface is testable in-process without building a binary.
package cli

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/JaydenCJ/logvert/internal/detect"
	"github.com/JaydenCJ/logvert/internal/event"
	"github.com/JaydenCJ/logvert/internal/parse"
	"github.com/JaydenCJ/logvert/internal/tsnorm"
	"github.com/JaydenCJ/logvert/internal/version"
)

// Exit codes, documented in the README.
const (
	ExitOK      = 0 // every line converted
	ExitStrict  = 1 // --strict was set and at least one line stayed raw
	ExitUsage   = 2 // bad flags or arguments
	ExitRuntime = 3 // I/O failure (unreadable file, broken pipe)
)

// multiFlag is a repeatable string flag.
type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

type config struct {
	format     string
	flat       bool
	strict     bool
	dropRaw    bool
	stats      bool
	assumeTZ   string
	assumeYear int
	maxLine    int
	maps       multiFlag
	dropFields multiFlag
	files      []string
}

// Run dispatches argv and returns the process exit code.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "version", "--version", "-v":
			fmt.Fprintf(stdout, "logvert %s\n", version.Version)
			return ExitOK
		case "help", "--help", "-h":
			usage(stdout)
			return ExitOK
		}
	}

	fs := flag.NewFlagSet("logvert", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cfg := config{}
	fs.StringVar(&cfg.format, "format", "auto", "input format: "+detect.FormatList())
	fs.BoolVar(&cfg.flat, "flat", false, "merge extra fields into the top level instead of nesting under \"fields\"")
	fs.BoolVar(&cfg.strict, "strict", false, "exit 1 if any line fails to parse (raw fallback)")
	fs.BoolVar(&cfg.dropRaw, "drop-raw", false, "discard lines no parser accepts instead of emitting raw events")
	fs.BoolVar(&cfg.stats, "stats", false, "print per-format line counts to stderr when done")
	fs.StringVar(&cfg.assumeTZ, "assume-tz", "UTC", "zone for timestamps without one: UTC or a fixed offset like +09:00")
	fs.IntVar(&cfg.assumeYear, "assume-year", 0, "year for timestamps without one (BSD syslog); 0 = current year")
	fs.IntVar(&cfg.maxLine, "max-line-bytes", 1<<20, "longest input line accepted, in bytes")
	fs.Var(&cfg.maps, "map", "rename a field, from=to; a canonical target (ts, level, msg, host, app, pid) lifts into the envelope (repeatable)")
	fs.Var(&cfg.dropFields, "drop-field", "remove this extra field from every event (repeatable)")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(stderr, "logvert: %v\n\n", err)
		usage(stderr)
		return ExitUsage
	}
	cfg.files = fs.Args()

	if !detect.ValidFormat(cfg.format) {
		fmt.Fprintf(stderr, "logvert: unknown --format %q (want %s)\n", cfg.format, detect.FormatList())
		return ExitUsage
	}
	loc, err := tsnorm.ParseOffset(cfg.assumeTZ)
	if err != nil {
		fmt.Fprintf(stderr, "logvert: --assume-tz: %v\n", err)
		return ExitUsage
	}
	if cfg.maxLine < 1024 {
		fmt.Fprintf(stderr, "logvert: --max-line-bytes must be at least 1024\n")
		return ExitUsage
	}
	mapper, err := newMapper(cfg.maps, cfg.dropFields)
	if err != nil {
		fmt.Fprintf(stderr, "logvert: %v\n", err)
		return ExitUsage
	}

	tsOpt := tsnorm.Options{Loc: loc, Year: cfg.assumeYear}
	if tsOpt.Year == 0 {
		tsOpt.Year = tsnorm.Default().Year
	}
	p := pipeline{
		cfg:    cfg,
		opt:    parse.Options{TS: tsOpt},
		mapper: mapper,
		out:    bufio.NewWriter(stdout),
		errw:   stderr,
		counts: map[string]int{},
	}

	code := ExitOK
	if len(cfg.files) == 0 {
		if err := p.consume(stdin); err != nil {
			fmt.Fprintf(stderr, "logvert: stdin: %v\n", err)
			code = ExitRuntime
		}
	} else {
		for _, name := range cfg.files {
			f, err := os.Open(name)
			if err != nil {
				fmt.Fprintf(stderr, "logvert: %v\n", err)
				code = ExitRuntime
				break
			}
			err = p.consume(f)
			f.Close()
			if err != nil {
				fmt.Fprintf(stderr, "logvert: %s: %v\n", name, err)
				code = ExitRuntime
				break
			}
		}
	}
	if err := p.out.Flush(); err != nil && code == ExitOK {
		fmt.Fprintf(stderr, "logvert: write: %v\n", err)
		code = ExitRuntime
	}
	if cfg.stats {
		p.printStats()
	}
	if code == ExitOK && cfg.strict && p.counts[event.SourceRaw] > 0 {
		n := p.counts[event.SourceRaw]
		fmt.Fprintf(stderr, "logvert: strict: %d line%s failed to parse\n", n, plural(n))
		code = ExitStrict
	}
	return code
}

// pipeline is the per-run state: options, counters, and the output writer.
type pipeline struct {
	cfg    config
	opt    parse.Options
	mapper *mapper
	out    *bufio.Writer
	errw   io.Writer
	counts map[string]int
	total  int
	blank  int
}

// consume converts every line of r to JSONL on p.out.
func (p *pipeline) consume(r io.Reader) error {
	sc := bufio.NewScanner(r)
	// The initial buffer must not exceed maxLine: bufio.Scanner treats the
	// larger of cap(buf) and max as the real limit, so a bigger starting
	// buffer would silently defeat small --max-line-bytes values.
	initial := 64 * 1024
	if p.cfg.maxLine < initial {
		initial = p.cfg.maxLine
	}
	sc.Buffer(make([]byte, 0, initial), p.cfg.maxLine)
	for sc.Scan() {
		line := strings.TrimSuffix(sc.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			p.blank++
			continue
		}
		p.total++
		ev := detect.Line(line, p.cfg.format, p.opt)
		p.counts[ev.Source]++
		if ev.Source == event.SourceRaw && p.cfg.dropRaw {
			continue
		}
		p.mapper.apply(&ev, p.opt)
		b, err := event.Encode(ev, p.cfg.flat)
		if err != nil {
			return err
		}
		if _, err := p.out.Write(b); err != nil {
			return err
		}
		if err := p.out.WriteByte('\n'); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return fmt.Errorf("line longer than --max-line-bytes (%d); raise the limit to convert it", p.cfg.maxLine)
		}
		return err
	}
	return nil
}

// printStats writes the per-format summary to stderr in a fixed order so
// the report is stable for tests and scripts.
func (p *pipeline) printStats() {
	parts := []string{}
	order := append(append([]string{}, detect.Formats...), event.SourceRaw)
	for _, name := range order {
		if n := p.counts[name]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", name, n))
		}
	}
	line := fmt.Sprintf("logvert: %d line%s in", p.total, plural(p.total))
	if len(parts) > 0 {
		line += " — " + strings.Join(parts, ", ")
	}
	if p.blank > 0 {
		line += fmt.Sprintf(" (%d blank line%s skipped)", p.blank, plural(p.blank))
	}
	fmt.Fprintln(p.errw, line)
}

// plural returns the "s" for counts other than one, so messages read
// "1 line" and "2 lines" instead of "line(s)".
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `logvert %s — convert mixed-format logs into one normalized JSONL stream

Usage:
  logvert [flags] [file ...]        read files, or stdin when none given
  logvert version                   print the version

Flags:
  --format NAME        input format (default auto): %s
  --flat               merge extra fields into the top level (collisions get a "_" prefix)
  --strict             exit 1 if any line fails to parse
  --drop-raw           discard unparseable lines instead of emitting raw events
  --map FROM=TO        rename a field; canonical targets (ts, level, msg,
                       host, app, pid) lift into the envelope (repeatable)
  --drop-field KEY     remove this extra field from every event (repeatable)
  --assume-tz ZONE     zone for zone-less timestamps (default UTC), e.g. +09:00
  --assume-year YYYY   year for BSD syslog timestamps (default: current year)
  --max-line-bytes N   longest accepted input line (default 1048576)
  --stats              print per-format line counts to stderr when done

Exit codes: 0 ok, 1 strict failure, 2 usage error, 3 I/O error.
`, version.Version, detect.FormatList())
}

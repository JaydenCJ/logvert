# Contributing to logvert

Issues, discussions and pull requests are all welcome.

## Getting started

You need Go ≥1.22; nothing else.

```bash
git clone https://github.com/JaydenCJ/logvert && cd logvert
go build ./...
go test ./...
bash scripts/smoke.sh
```

`scripts/smoke.sh` builds the binary, pushes a deterministic mixed
eight-format stream through it, and asserts on the real CLI output —
detection, timestamp normalization, `--map`, `--strict`, exit codes; it
must finish by printing `SMOKE OK`.

## Before you open a pull request

1. `gofmt -l .` reports nothing (formatting is enforced).
2. `go vet ./...` passes with no findings.
3. `go test ./...` passes (90 deterministic tests, no network).
4. `bash scripts/smoke.sh` prints `SMOKE OK`.
5. Add tests for behavior changes; keep logic in pure, unit-testable
   modules (parsers are string→Event functions and never touch I/O —
   only `internal/cli` reads or writes streams).

## Ground rules

- Keep dependencies at zero; adding one needs strong justification in
  the PR.
- No network calls, ever, and no telemetry. logvert reads stdin/files and
  writes stdout/stderr — that is its entire I/O surface.
- Determinism first: identical input must produce byte-identical output,
  including field order. New parsers must be attempted-parse detectors
  (accept fully or reject cleanly), never partial regex matches.
- A new format needs: a parser in `internal/parse`, a detection-order
  entry with a false-positive test, a mapping table row in
  `docs/schema.md`, and a line in `examples/mixed.log`.
- Code comments and doc comments are written in English.

## Reporting bugs

Include the output of `logvert version`, the exact command line, one
sanitized input line that reproduces the problem, and the JSONL you got
versus what you expected. For misdetections, note which `--format` value
produces the right result.

## Security

Please do not open public issues for security problems; use GitHub's
private vulnerability reporting on this repository instead.

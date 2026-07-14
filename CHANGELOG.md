# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-07-12

### Added

- Normalized JSONL envelope (`ts`, `level`, `msg`, `host`, `app`, `pid`,
  `source`, `fields`) with a deterministic encoder: fixed key order,
  source-order extra fields, no HTML escaping, byte-identical output for
  identical input.
- logfmt parser: quoted values with escapes (`\"`, `\n`, `\uXXXX`), bare
  boolean keys, empty values, JSON typing for unquoted numbers/booleans,
  and a strict token gate so prose is never misclassified.
- syslog parser for both generations: RFC 5424 (header fields, structured
  data with `\"` `\\` `\]` escapes flattened as `sd-id.param`, BOM
  stripping) and RFC 3164/BSD (`tag[pid]:` extraction), each with or
  without the `<PRI>` header, decoding PRI into level + facility.
- Apache/nginx access-log parser (Common and Combined formats) with
  request splitting and HTTP-status→level derivation (5xx→error, 4xx→warn).
- Error-log parsers for nginx (worker `pid#tid`, connection id, trailing
  `, key: value` context pairs promoted to fields) and Apache 2.2/2.4
  (`module:level`, `pid/tid`, client, `AHnnnnn` codes).
- Timestamp normalization to RFC 3339 UTC from RFC 3339/ISO 8601, epoch
  seconds/millis/micros/nanos, CLF, BSD syslog, and both error-log date
  shapes, with `--assume-tz` / `--assume-year` for under-specified inputs.
- Level normalization onto a six-value scale (trace…fatal) from text
  aliases, syslog severities, and numeric logger levels; unknown spellings
  are preserved as `level_raw`, never guessed.
- Per-line auto-detection by attempted parse (`--format auto`, the
  default) plus forced single-format mode; unmatched lines become `raw`
  events so no data is ever dropped silently.
- Pipe-stage CLI: stdin or files, `--flat`, `--strict`, `--drop-raw`,
  `--map from=to` (with canonical lifting), `--drop-field`, `--stats`,
  `--max-line-bytes`; exit codes 0/1/2/3.
- Runnable examples (`examples/mixed.log`, `examples/filter-errors.sh`)
  and a schema + field-mapping reference (`docs/schema.md`).
- 90 deterministic offline tests (unit + in-process CLI) and
  `scripts/smoke.sh`.

[0.1.0]: https://github.com/JaydenCJ/logvert/releases/tag/v0.1.0

# The normalized event schema

Every input line becomes exactly one JSON object on stdout (unless
`--drop-raw` discards it). Keys appear in a fixed order, so identical
input always produces byte-identical output.

## Envelope

| Key | Type | Present | Meaning |
|---|---|---|---|
| `ts` | string | when the line carried a timestamp | RFC 3339 in UTC, sub-seconds preserved |
| `level` | string | when a severity was found and recognized | one of `trace` `debug` `info` `warn` `error` `fatal` |
| `msg` | string | always | the human-readable message (may be empty) |
| `host` | string | when present | originating hostname |
| `app` | string | when present | program / service / logger name |
| `pid` | number | when present | process id |
| `source` | string | always | which parser matched: `json` `logfmt` `syslog` `access` `nginx-error` `apache-error` `raw` |
| `fields` | object | when any extras remain | everything else, in source order |

With `--flat` the extras merge into the top level instead; an extra key
that would shadow an envelope key is emitted with a `_` prefix
(`_source`), never dropped.

## Level normalization

Text aliases fold onto the six-value scale (`warning`→`warn`,
`err`→`error`, `notice`→`info`, `crit`/`alert`/`emerg`/`panic`→`fatal`,
…). Syslog numeric severities 0–7 and the 10–60 numeric convention of
popular structured loggers map likewise. HTTP statuses derive levels for
access logs: 5xx→`error`, 4xx→`warn`, else `info`. An unrecognized
spelling is never guessed: `level` is omitted and the original value is
preserved as `fields.level_raw`.

## Timestamp normalization

Accepted dialects: RFC 3339 / ISO 8601 (any zone, `T` or space), Unix
epoch in seconds/millis/micros/nanos (unit chosen by magnitude), CLF
(`10/Oct/2000:13:55:36 -0700`), BSD syslog (`Jul 12 10:00:03` —
year from `--assume-year`), nginx (`2026/07/12 10:00:05`) and Apache
(`Sun Jul 12 10:00:06.123456 2026`) error dates. Zone-less values are
interpreted in `--assume-tz` (default UTC). Everything is emitted as
RFC 3339 UTC.

## Per-format field mapping

### `json` and `logfmt` (key-based lifting)

The first alias found wins; later aliases stay in `fields` untouched.

| Envelope key | Lifted from (priority order) |
|---|---|
| `ts` | `ts`, `time`, `timestamp`, `@timestamp`, `datetime` |
| `level` | `level`, `lvl`, `severity`, `loglevel` |
| `msg` | `msg`, `message` |
| `host` | `host`, `hostname` |
| `app` | `app`, `service`, `logger`, `program`, `name` |
| `pid` | `pid` |

Anything else (e.g. Docker's `log`, an in-house `severity_text`) lifts
with `--map log=msg --map severity_text=level`.

### `syslog`

| Input | Lands in |
|---|---|
| PRI severity (0–7) | `level` |
| PRI facility | `fields.facility` |
| HOSTNAME | `host` |
| APP-NAME / TAG | `app` |
| PROCID / `[pid]` | `pid` (non-numeric PROCIDs → `fields.procid`) |
| MSGID | `fields.msgid` |
| SD-PARAM `k="v"` in `[sdid …]` | `fields."sdid.k"` |

### `access` (Common / Combined)

| Input | Lands in |
|---|---|
| request line | `msg`, plus `fields.method` / `path` / `proto` |
| status | `fields.status`, and derives `level` |
| remote host, authuser | `fields.remote_addr`, `fields.user` |
| bytes, referer, user-agent | `fields.bytes` / `referer` / `user_agent` (`-` omitted) |

### `nginx-error` / `apache-error`

| Input | Lands in |
|---|---|
| bracketed severity | `level` |
| `pid#tid` / `[pid P:tid T]` | `pid`, `fields.tid` |
| nginx `, client: …, server: …, request: …` suffixes | fields of the same name |
| nginx `*N` connection id | `fields.connection` |
| Apache `[module:level]`, `[client …]`, `AHnnnnn:` | `fields.module` / `client` / `code` |

### `raw`

The whole line becomes `msg`; nothing else is set. With `--strict` the
run then exits 1, with `--drop-raw` the line is discarded instead.

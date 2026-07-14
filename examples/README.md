# logvert examples

Everything here is offline and deterministic.

## mixed.log

One line of every format logvert understands — logfmt, JSON, RFC 5424 and
BSD syslog, a Combined access line, nginx and Apache error lines, and one
unstructured crash line — exactly the kind of stream a shared host or a
`docker compose logs` pipe produces.

```bash
logvert --assume-year 2026 examples/mixed.log
logvert --assume-year 2026 --stats examples/mixed.log > /dev/null   # just the counts
```

`--assume-year 2026` pins the year for the BSD syslog line (which carries
none), so the output is byte-identical on every machine, every year.

## filter-errors.sh

A complete pipe-stage recipe: normalize the mixed stream, keep only
error-and-worse events, and print `ts`, `source`, and `msg` — no jq
required, because the envelope keys are ordinary top-level JSON keys.

```bash
bash examples/filter-errors.sh
```

## Remapping a nonstandard producer

Docker's json-file driver writes the message under `log`; an in-house app
might write `severity_text`. `--map` fixes both without a config file:

```bash
logvert --map log=msg --map severity_text=level app.jsonl
```

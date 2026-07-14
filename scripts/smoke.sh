#!/usr/bin/env bash
# End-to-end smoke test for logvert: builds the binary, pushes a mixed
# eight-format stream through it, and asserts on the real CLI output.
# No network, idempotent, finishes in seconds.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

fail() {
  echo "SMOKE FAIL: $*" >&2
  exit 1
}

BIN="$WORKDIR/logvert"

echo "1. build"
(cd "$ROOT" && go build -o "$BIN" ./cmd/logvert) || fail "go build failed"

echo "2. version matches manifest"
"$BIN" --version | grep -qx "logvert 0.1.0" || fail "--version mismatch"

echo "3. mixed stream: every line lands on its own parser"
OUT="$("$BIN" --assume-year 2026 "$ROOT/examples/mixed.log")"
[ "$(printf '%s\n' "$OUT" | wc -l)" -eq 8 ] || fail "want 8 output lines"
for src in json logfmt syslog access nginx-error apache-error raw; do
  printf '%s\n' "$OUT" | grep -q "\"source\":\"$src\"" || fail "no $src event in output"
done

echo "4. timestamps are normalized to RFC 3339 UTC"
printf '%s\n' "$OUT" | grep -q '"ts":"2026-07-12T10:00:03Z"' \
  || fail "BSD syslog timestamp not normalized (assume-year)"
printf '%s\n' "$OUT" | grep -q '"ts":"2026-07-12T10:00:01.25Z"' \
  || fail "JSON millisecond timestamp not normalized"

echo "5. access-log status derives the level"
printf '%s\n' "$OUT" | grep '"source":"access"' | grep -q '"level":"error"' \
  || fail "HTTP 500 should map to level error"

echo "6. nginx context pairs become fields"
printf '%s\n' "$OUT" | grep '"source":"nginx-error"' | grep -q '"server":"example.test"' \
  || fail "nginx ', server: …' not promoted to a field"

echo "7. --map lifts nonstandard keys into the envelope"
echo '{"severity_text":"WARN","svc":"pay","msg":"slow"}' \
  | "$BIN" --map severity_text=level --map svc=app \
  | grep -q '"level":"warn","msg":"slow","app":"pay"' \
  || fail "--map lifting failed"

echo "8. --strict exits 1 on unparseable input, 0 on clean input"
if echo "total gibberish" | "$BIN" --strict >/dev/null 2>&1; then
  fail "--strict should exit 1 on a raw line"
fi
echo 'level=info msg=ok' | "$BIN" --strict >/dev/null 2>&1 \
  || fail "--strict should exit 0 on clean input"

echo "9. --stats reports per-format counts on stderr"
STATS="$("$BIN" --assume-year 2026 --stats "$ROOT/examples/mixed.log" 2>&1 >/dev/null)"
echo "$STATS" | grep -q "8 lines in" || fail "stats total wrong: $STATS"
echo "$STATS" | grep -q "raw 1" || fail "stats raw count wrong: $STATS"

echo "10. output is byte-identical across runs"
"$BIN" --assume-year 2026 "$ROOT/examples/mixed.log" > "$WORKDIR/a.jsonl"
"$BIN" --assume-year 2026 "$ROOT/examples/mixed.log" > "$WORKDIR/b.jsonl"
cmp -s "$WORKDIR/a.jsonl" "$WORKDIR/b.jsonl" || fail "nondeterministic output"

echo "11. usage errors exit 2"
set +e
"$BIN" --format yaml < /dev/null >/dev/null 2>&1
[ $? -eq 2 ] || fail "bad --format should exit 2"
set -e

echo "12. example pipe stage finds the four error-and-worse events"
N="$(LOGVERT="$BIN" bash "$ROOT/examples/filter-errors.sh" | wc -l)"
[ "$N" -eq 4 ] || fail "filter-errors.sh should print 4 lines, got $N"

echo "SMOKE OK"

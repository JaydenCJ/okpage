#!/usr/bin/env bash
# End-to-end smoke test for okpage: builds the binary, scaffolds a project,
# probes a loopback HTTP server plus a deliberately dead port, and asserts on
# the real CLI output and the generated site. No external network, idempotent,
# finishes in seconds.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKDIR="$(mktemp -d)"
SERVER_PID=""
cleanup() {
  [ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null || true
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

fail() {
  echo "SMOKE FAIL: $*" >&2
  exit 1
}

BIN="$WORKDIR/okpage"
SITE="$WORKDIR/site"

echo "1. build"
(cd "$ROOT" && go build -o "$BIN" ./cmd/okpage) || fail "go build failed"

echo "2. version matches manifest"
"$BIN" --version | grep -qx "okpage 0.1.0" || fail "--version mismatch"

echo "3. init scaffolds a valid project"
"$BIN" init "$SITE" >/dev/null || fail "init failed"
[ -f "$SITE/okpage.toml" ] || fail "okpage.toml not created"
ls "$SITE/incidents/"*-welcome.md >/dev/null 2>&1 || fail "welcome incident not created"
if "$BIN" init "$SITE" >/dev/null 2>&1; then
  fail "second init should refuse to overwrite"
fi

echo "4. start a loopback health endpoint"
cat > "$WORKDIR/server.go" <<'EOF'
// Minimal loopback HTTP server for the smoke test; prints its address.
package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
)

func main() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(os.Args[1], []byte(ln.Addr().String()), 0o644); err != nil {
		panic(err)
	}
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"ok"}`)
	})
	_ = http.Serve(ln, nil)
}
EOF
# Build first and run the binary directly, so SERVER_PID is the server
# itself (a `go run` child would survive the kill and hold pipes open).
(cd "$WORKDIR" && go build -o smokeserver server.go) || fail "server build failed"
"$WORKDIR/smokeserver" "$WORKDIR/addr" &
SERVER_PID=$!
for _ in $(seq 1 100); do [ -s "$WORKDIR/addr" ] && break; sleep 0.1; done
[ -s "$WORKDIR/addr" ] || fail "loopback server did not start"
ADDR="$(cat "$WORKDIR/addr")"

echo "5. write a config with one healthy and one dead service"
cat > "$SITE/okpage.toml" <<EOF
title = "Smoke Status"
timeout = "5s"

[[service]]
name = "API"
type = "http"
url = "http://$ADDR/health"
expect_body = "ok"

[[service]]
name = "TCP door"
type = "tcp"
address = "$ADDR"

[[service]]
name = "Dead"
type = "tcp"
address = "127.0.0.1:1"
EOF

echo "6. check reports mixed fleet and exits 1"
set +e
OUT="$("$BIN" check -c "$SITE/okpage.toml")"
CODE=$?
set -e
[ "$CODE" -eq 1 ] || fail "check with a down service should exit 1, got $CODE"
echo "$OUT" | grep -q "up  API" || fail "API should be up"
echo "$OUT" | grep -q "DOWN  Dead" || fail "Dead should be down"
echo "$OUT" | grep -q "2 up, 1 down" || fail "summary line wrong"

echo "7. history is recorded as JSON lines"
[ "$(wc -l < "$SITE/history.jsonl")" -eq 3 ] || fail "expected 3 history records"
grep -q '"service":"API"' "$SITE/history.jsonl" || fail "API record missing"

echo "8. build renders the static site"
BUILD_OUT="$("$BIN" build -c "$SITE/okpage.toml")" || fail "build failed"
echo "$BUILD_OUT" | grep -q "wrote 5 files" || fail "build output wrong: $BUILD_OUT"
grep -q "Partial outage" "$SITE/public/index.html" || fail "banner missing"
grep -q "Smoke Status" "$SITE/public/index.html" || fail "title missing"
grep -q '"overall": "degraded"' "$SITE/public/status.json" || fail "status.json overall wrong"
grep -q '>up</text>' "$SITE/public/badge/api.svg" || fail "up badge wrong"
grep -q '>down</text>' "$SITE/public/badge/dead.svg" || fail "down badge wrong"
# Rebuilding with unchanged inputs must be byte-identical (deterministic build).
cp "$SITE/public/index.html" "$WORKDIR/first.html"
cp "$SITE/public/status.json" "$WORKDIR/first.json"
"$BIN" build -c "$SITE/okpage.toml" >/dev/null || fail "second build failed"
cmp -s "$SITE/public/index.html" "$WORKDIR/first.html" || fail "index.html not deterministic"
cmp -s "$SITE/public/status.json" "$WORKDIR/first.json" || fail "status.json not deterministic"

echo "9. incidents render onto the page"
cat > "$SITE/incidents/2026-07-11-dead-port.md" <<'EOF'
---
title: Dead service unreachable
date: 2026-07-11T08:00:00Z
status: investigating
affected: [Dead]
---

The **Dead** service refuses connections. Digging in.
EOF
"$BIN" build -c "$SITE/okpage.toml" >/dev/null || fail "rebuild failed"
grep -q "Dead service unreachable" "$SITE/public/index.html" || fail "incident title missing"
grep -q "<strong>Dead</strong>" "$SITE/public/index.html" || fail "incident markdown not rendered"
grep -q '"status": "investigating"' "$SITE/public/status.json" || fail "incident missing from status.json"

echo "10. checks accumulate history"
"$BIN" check --quiet -c "$SITE/okpage.toml" >/dev/null 2>&1 || true
[ "$(wc -l < "$SITE/history.jsonl")" -eq 6 ] || fail "history should accumulate to 6 records"

echo "11. usage errors exit 2"
set +e
"$BIN" frobnicate >/dev/null 2>&1
[ $? -eq 2 ] || fail "unknown command should exit 2"
"$BIN" check extra-arg >/dev/null 2>&1
[ $? -eq 2 ] || fail "stray argument should exit 2"
set -e

echo "SMOKE OK"

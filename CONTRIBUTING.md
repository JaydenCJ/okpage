# Contributing to okpage

Issues, discussions and pull requests are all welcome.

## Getting started

You need Go ≥1.22; nothing else.

```bash
git clone https://github.com/JaydenCJ/okpage && cd okpage
go build ./...
go test ./...
bash scripts/smoke.sh
```

`scripts/smoke.sh` builds the binary, scaffolds a project in a temp dir,
probes a loopback server plus a deliberately dead port, and asserts on the
real CLI output and the generated site; it must finish by printing `SMOKE OK`.

## Before you open a pull request

1. `gofmt -l .` reports nothing (formatting is enforced).
2. `go vet ./...` passes with no findings.
3. `go test ./...` passes (90 deterministic tests, no external network).
4. `bash scripts/smoke.sh` prints `SMOKE OK`.
5. Add tests for behavior changes; keep logic in pure, unit-testable
   modules (`stats`, `site`, and the parsers never touch the network —
   only `probe` does, behind injectable interfaces).

## Ground rules

- Keep dependencies at zero — okpage is standard library only, and staying
  that way is the point. Adding one needs strong justification in the PR.
- The only network okpage ever touches is the services the user configured.
  No telemetry, no update checks, nothing at startup.
- `build` must stay a pure function: identical config + history + incidents
  in, byte-identical site out. Ordering, formatting, and timestamps all come
  from inputs, never from map iteration or the wall clock.
- The generated page stays self-contained: no JavaScript, no external
  assets. Anything user-authored that lands in HTML goes through escaping.
- Code comments and doc comments are written in English.

## Reporting bugs

Include the output of `okpage version`, the full command you ran, your
`okpage.toml` (redact real hostnames if needed), and — for probe
misclassifications — the `detail` field from the affected `history.jsonl`
line, since that is exactly what the prober observed.

## Security

Please do not open public issues for security problems; use GitHub's
private vulnerability reporting on this repository instead.

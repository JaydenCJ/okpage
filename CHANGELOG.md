# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-07-13

### Added

- Three probe types with independent timeouts, run concurrently with
  config-ordered results: `http` (GET/HEAD, exact-status or any-2xx policy,
  body-substring assertion, 1 MiB read cap), `tcp` (connect), and `dns`
  (resolve to ≥1 address).
- `okpage.toml` configuration in a strict TOML subset with line-numbered
  errors, unknown-key rejection to catch typos, per-service timeout
  overrides, and paths resolved relative to the config file.
- Append-only JSON-lines probe history with atomic retention pruning
  (temp file + rename) and line-numbered corruption reporting.
- Uptime aggregation as pure functions: 24h/7d/90d rolling windows,
  UTC-calendar-day buckets (up/degraded/down/no-data) for the bar strip,
  and page-level overall state.
- Incidents as markdown files with front matter (title, date, status,
  affected), rendered by a built-in sanitizing markdown subset: HTML
  escaped everywhere, dangerous link schemes neutralized.
- Static site output: self-contained `index.html` (inline CSS, zero
  JavaScript, light/dark themes), stable `status.json`
  (`schema_version: 1`, null-aware uptime), and shields-style per-service
  SVG badges — byte-identical output for identical inputs.
- CLI: `init` scaffolding, `check` (probe + record + prune, exit 1 on any
  failure, `--build`, `--quiet`), `build` (render only, never probes),
  `version`; exit codes 0/1/2/3.
- Runnable example project (`examples/`), a formats reference
  (`docs/formats.md`), 90 deterministic offline tests, and
  `scripts/smoke.sh`.

[0.1.0]: https://github.com/JaydenCJ/okpage/releases/tag/v0.1.0

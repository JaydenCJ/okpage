# okpage formats reference

Everything okpage reads and writes, in one place. All formats are plain
text; nothing needs a database or a migration tool.

## okpage.toml

A small TOML subset: `key = value` pairs (strings, integers, booleans,
one-line arrays), `[table]`, and `[[array-of-tables]]`. Multi-line strings,
floats, dates, dotted keys, and inline tables are intentionally rejected —
if a config parses, it parsed the way you meant it. Unknown keys are errors,
so typos like `expect_staus` cannot silently disable a check.

### Top-level keys

| Key | Default | Effect |
|---|---|---|
| `title` | `"Status"` | page heading and `<title>` |
| `output` | `"public"` | directory the static site is written to |
| `history` | `"history.jsonl"` | probe history file |
| `incidents` | `"incidents"` | directory holding incident markdown files |
| `retention_days` | `90` | history records older than this are pruned by `check` |
| `days` | `90` | number of daily bars rendered per service (1–365) |
| `timeout` | `"10s"` | default per-probe timeout |

Relative paths resolve against the directory containing the config file,
so `okpage check -c /srv/status/okpage.toml` behaves identically from cron
and from an interactive shell.

### `[[service]]` keys

| Key | Applies to | Default | Effect |
|---|---|---|---|
| `name` | all | required | unique display name; also the badge slug |
| `type` | all | `"http"` | `http`, `tcp`, or `dns` |
| `timeout` | all | top-level `timeout` | per-service override |
| `url` | http | required | absolute `http://` or `https://` URL |
| `method` | http | `GET` | `GET` or `HEAD` |
| `expect_status` | http | any 2xx | exact status code required |
| `expect_body` | http | — | response body must contain this substring (first 1 MiB) |
| `address` | tcp | required | `host:port` to connect to |
| `hostname` | dns | required | name that must resolve to ≥1 address |

## history.jsonl

One JSON object per line, append-only. `check` appends one record per
service per run and prunes lines older than `retention_days` atomically
(temp file + rename).

```json
{"ts":"2026-07-12T09:00:00Z","service":"API","ok":true,"latency_ms":42,"detail":"200"}
```

JSONL means the file diffs cleanly in git, merges by concatenation (probe
from two hosts, `cat` the files together), and a crash mid-write can at
worst leave one partial line — which `build` reports with its line number
instead of guessing.

## Incident files

One markdown file per incident in the incidents directory. Front matter
first, body after:

```markdown
---
title: Elevated API latency
date: 2026-07-10T14:30:00Z
status: resolved
affected: [API, Website]
---

Markdown body…
```

| Field | Required | Values |
|---|---|---|
| `title` | yes | free text |
| `date` | yes | RFC 3339 or bare `YYYY-MM-DD` (midnight UTC) |
| `status` | yes | `investigating`, `identified`, `monitoring`, `resolved` |
| `affected` | no | `[A, B]` or bare comma list of service names |

The body supports a safe markdown subset: ATX headings (shifted two levels
to fit the page outline), paragraphs, `-`/`*` and `1.` lists, `>` quotes,
``` fences, and inline `code`, `**bold**`, `*italic*`, `[links](…)`. Raw
HTML is escaped, and link schemes other than http/https/mailto/relative are
neutralized, so an incident file can never inject script into your page.
Files named `README.md` and dotfiles are skipped.

## Output directory

| File | Contents |
|---|---|
| `index.html` | the whole page: inline CSS, no JavaScript, no external requests, light/dark via `prefers-color-scheme` |
| `status.json` | machine-readable state (`schema_version: 1`): overall, per-service state/latency/uptime (null when no data), incident list |
| `badge/<slug>.svg` | shields-style per-service badge (`up` green / `down` red / `no data` gray) for embedding in READMEs |

`build` is a pure function of (config, history, incidents): the page's
"as of" time (`as_of` in status.json) is the newest probe or incident
timestamp, never the wall clock, so identical inputs produce byte-identical
output — rebuilds are cheap to diff and safe to run anywhere.

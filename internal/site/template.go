package site

// The page template is embedded in the binary so the output is a single
// self-contained index.html: inline CSS, no JavaScript, no webfonts, no
// external requests of any kind. Light and dark themes follow the visitor's
// prefers-color-scheme.
const pageTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="generator" content="okpage {{version}}">
<title>{{.Title}}</title>
<style>
:root {
  --bg: #f6f8fa; --card: #ffffff; --text: #1f2328; --muted: #59636e;
  --border: #d1d9e0; --up: #1a7f37; --down: #cf222e; --degraded: #9a6700;
  --nodata: #d1d9e0; --banner-up: #dafbe1; --banner-down: #ffebe9;
  --banner-degraded: #fff8c5; --banner-unknown: #eaeef2;
}
@media (prefers-color-scheme: dark) {
  :root {
    --bg: #0d1117; --card: #161b22; --text: #e6edf3; --muted: #8b949e;
    --border: #30363d; --up: #3fb950; --down: #f85149; --degraded: #d29922;
    --nodata: #30363d; --banner-up: #12261e; --banner-down: #2d1214;
    --banner-degraded: #272115; --banner-unknown: #1c2128;
  }
}
* { box-sizing: border-box; }
body { margin: 0; background: var(--bg); color: var(--text);
  font: 16px/1.5 -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif; }
main { max-width: 720px; margin: 0 auto; padding: 2rem 1rem 3rem; }
h1 { font-size: 1.5rem; margin: 0 0 1rem; }
.banner { border-radius: 8px; padding: 0.9rem 1.2rem; font-weight: 600; margin-bottom: 1.5rem; }
.banner.operational { background: var(--banner-up); color: var(--up); }
.banner.outage { background: var(--banner-down); color: var(--down); }
.banner.degraded { background: var(--banner-degraded); color: var(--degraded); }
.banner.unknown { background: var(--banner-unknown); color: var(--muted); }
.card { background: var(--card); border: 1px solid var(--border); border-radius: 8px;
  padding: 1rem 1.2rem; margin-bottom: 0.8rem; }
.svc-head { display: flex; align-items: baseline; gap: 0.6rem; flex-wrap: wrap; }
.svc-name { font-weight: 600; }
.state { font-size: 0.85rem; font-weight: 600; }
.state.up { color: var(--up); }
.state.down { color: var(--down); }
.state.unknown { color: var(--muted); }
.latency, .detail { color: var(--muted); font-size: 0.85rem; }
.bars { display: flex; gap: 2px; margin: 0.7rem 0 0.4rem; }
.bars span { flex: 1; height: 26px; border-radius: 2px; background: var(--nodata); min-width: 2px; }
.bars .up { background: var(--up); }
.bars .down { background: var(--down); }
.bars .degraded { background: var(--degraded); }
.uptimes { display: flex; gap: 1.2rem; color: var(--muted); font-size: 0.8rem; }
h2 { font-size: 1.15rem; margin: 2rem 0 0.8rem; }
article.incident { background: var(--card); border: 1px solid var(--border);
  border-radius: 8px; padding: 1rem 1.2rem; margin-bottom: 0.8rem; }
.inc-head { display: flex; align-items: baseline; gap: 0.6rem; flex-wrap: wrap; }
.inc-title { font-weight: 600; }
.pill { font-size: 0.75rem; font-weight: 600; padding: 0.1rem 0.55rem;
  border-radius: 999px; border: 1px solid var(--border); }
.pill.resolved { color: var(--up); }
.pill.investigating, .pill.identified { color: var(--down); }
.pill.monitoring { color: var(--degraded); }
.inc-meta { color: var(--muted); font-size: 0.8rem; margin: 0.2rem 0 0.6rem; }
.inc-body { font-size: 0.9rem; }
.inc-body pre { background: var(--bg); border: 1px solid var(--border);
  border-radius: 6px; padding: 0.7rem; overflow-x: auto; }
.inc-body code { background: var(--bg); padding: 0.1rem 0.3rem; border-radius: 4px; }
.inc-body pre code { padding: 0; }
.inc-body blockquote { border-left: 3px solid var(--border); margin: 0.5rem 0; padding: 0 0 0 0.8rem; color: var(--muted); }
footer { margin-top: 2.5rem; color: var(--muted); font-size: 0.8rem; text-align: center; }
footer a { color: inherit; }
</style>
</head>
<body>
<main>
<h1>{{.Title}}</h1>
<div class="banner {{.Overall}}">{{overallText .Overall}}</div>

{{range .Services}}<section class="card" id="{{slug .Name}}">
  <div class="svc-head">
    <span class="svc-name">{{.Name}}</span>
    {{if .HasData}}{{if .LastOK}}<span class="state up">● up</span><span class="latency">{{.LastLatency}} ms</span>{{else}}<span class="state down">● down</span><span class="detail">{{.LastDetail}}</span>{{end}}{{else}}<span class="state unknown">● no data</span>{{end}}
  </div>
  <div class="bars" role="img" aria-label="daily availability, oldest to newest">
    {{range .Days}}<span class="{{.State}}" title="{{dayTitle .}}"></span>{{end}}
  </div>
  <div class="uptimes">
    <span>24h {{.Uptime24h}}</span>
    <span>7d {{.Uptime7d}}</span>
    <span>90d {{.Uptime90d}}</span>
    {{if .HasData}}<span>checked {{utc .LastChecked}}</span>{{end}}
  </div>
</section>
{{end}}
{{if .Incidents}}<h2>Incidents</h2>
{{range .Incidents}}<article class="incident" id="incident-{{.Slug}}">
  <div class="inc-head">
    <span class="inc-title">{{.Title}}</span>
    <span class="pill {{.Status}}">{{.Status}}</span>
  </div>
  <div class="inc-meta">{{day .Date}}{{if .Affected}} · affects {{join .Affected}}{{end}}</div>
  <div class="inc-body">{{html .BodyHTML}}</div>
</article>
{{end}}{{end}}
<footer>Data as of {{utc .Now}} · <a href="https://github.com/JaydenCJ/okpage">okpage</a> {{version}} · <a href="status.json">status.json</a></footer>
</main>
</body>
</html>
`

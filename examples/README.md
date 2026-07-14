# okpage examples

A complete example project: a realistic config and one resolved incident.

## Try it

The probes point at `example.test` and local ports, so on most machines the
fleet will show as down — which is itself a useful demo of the failure
rendering:

```bash
go build -o okpage ./cmd/okpage
./okpage check --build -c examples/okpage.toml
# open examples/public/index.html in a browser
```

`check` writes `examples/history.jsonl` and `examples/public/` because paths
in a config resolve relative to the config file. Delete both to reset.

## Files

- `okpage.toml` — six services across all three probe types (http with
  status/body expectations, tcp, dns), with per-service timeouts.
- `incidents/2026-07-10-elevated-api-latency.md` — a resolved incident
  showing the front matter format and the supported markdown subset
  (headings, lists, ordered timeline).

## Cron pairing

The intended production loop is two lines of crontab:

```cron
*/5 * * * *  cd /srv/status && /usr/local/bin/okpage check --build --quiet
7 3 * * *    cd /srv/status && rsync -a public/ deploy@web:/var/www/status/
```

Any sync mechanism works in the second slot — `git commit && git push` to a
Pages branch, `aws s3 sync`, or nothing at all if the web server reads
`public/` directly.

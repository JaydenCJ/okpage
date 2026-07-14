package cli

import (
	"fmt"
	"os"
	"path/filepath"
)

const initConfig = `# okpage configuration — see https://github.com/JaydenCJ/okpage
title = "My Status"

# Where the static site is written. Push this directory to any host.
output = "public"

# Probe history (JSON lines) and incident markdown files.
history = "history.jsonl"
incidents = "incidents"
retention_days = 90

# Default timeout for every probe; override per service if needed.
timeout = "10s"

[[service]]
name = "Website"
type = "http"
url = "http://127.0.0.1:8080/health"
# expect_status = 200        # exact status; default accepts any 2xx
# expect_body = "ok"         # response body must contain this substring

[[service]]
name = "Database"
type = "tcp"
address = "127.0.0.1:5432"

[[service]]
name = "DNS"
type = "dns"
hostname = "example.test"
`

const initIncident = `---
title: Welcome to your status page
date: %s
status: resolved
affected: []
---

This is an example incident. Each incident is a markdown file in the
` + "`incidents/`" + ` directory — create one per outage, update its ` + "`status`" + `
as things progress, and rebuild the page.

Delete this file whenever you like.
`

// cmdInit scaffolds a working setup: a commented config and one example
// incident. It refuses to overwrite an existing config so a stray `init`
// in the wrong directory cannot destroy anything.
func (a *App) cmdInit(args []string) error {
	if len(args) > 1 {
		return usageErr("init: expected at most one directory argument")
	}
	dir := "."
	if len(args) == 1 {
		dir = args[0]
	}

	cfgPath := filepath.Join(dir, "okpage.toml")
	if _, err := os.Stat(cfgPath); err == nil {
		return usageErr("init: %s already exists — not overwriting", cfgPath)
	}

	if err := os.MkdirAll(filepath.Join(dir, "incidents"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(cfgPath, []byte(initConfig), 0o644); err != nil {
		return err
	}

	date := a.Now().UTC().Format("2006-01-02")
	incidentPath := filepath.Join(dir, "incidents", date+"-welcome.md")
	if err := os.WriteFile(incidentPath, []byte(fmt.Sprintf(initIncident, date)), 0o644); err != nil {
		return err
	}

	fmt.Fprintf(a.Stdout, "created %s\n", cfgPath)
	fmt.Fprintf(a.Stdout, "created %s\n", incidentPath)
	fmt.Fprintf(a.Stdout, "\nnext: edit %s, then run `okpage check --build`\n", cfgPath)
	return nil
}

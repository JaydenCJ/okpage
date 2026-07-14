package cli

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/JaydenCJ/okpage/internal/config"
	"github.com/JaydenCJ/okpage/internal/history"
	"github.com/JaydenCJ/okpage/internal/incident"
	"github.com/JaydenCJ/okpage/internal/site"
	"github.com/JaydenCJ/okpage/internal/stats"
)

// cmdBuild renders the static site from whatever history and incidents are
// on disk. It never probes anything, so it is safe to run from a different
// machine than the one doing the checks (e.g. render on the web host after
// rsyncing history.jsonl over).
func (a *App) cmdBuild(args []string) error {
	fs := newFlagSet("build")
	cfgPath := configFlag(fs)
	if err := fs.Parse(args); err != nil {
		return usageErr("build: %v", err)
	}
	if fs.NArg() > 0 {
		return usageErr("build: unexpected argument %q", fs.Arg(0))
	}

	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		return err
	}
	return a.build(cfg, false)
}

// build assembles page data and writes the site; shared by `build` and
// `check --build`.
//
// The page's "as of" time is anchored to the inputs — the newest probe
// record, falling back to the newest incident date — never to the wall
// clock, so identical inputs render byte-identical sites (the contract in
// docs/formats.md). Only a project with no data at all uses the clock.
func (a *App) build(cfg *config.Config, quiet bool) error {
	records, err := history.Load(cfg.History)
	if err != nil {
		return fmt.Errorf("loading history: %w", err)
	}

	incidents, err := incident.LoadDir(cfg.Incidents)
	if err != nil {
		return fmt.Errorf("loading incidents: %w", err)
	}

	names := make([]string, 0, len(cfg.Services))
	for _, svc := range cfg.Services {
		names = append(names, svc.Name)
	}

	now := anchorTime(records, incidents, a.Now)
	serviceStats := stats.Compute(records, names, now, cfg.Days)

	files, err := site.Build(cfg.Output, site.Data{
		Title:     cfg.Title,
		Now:       now,
		Overall:   stats.Overall(serviceStats),
		Services:  serviceStats,
		Incidents: incidents,
	})
	if err != nil {
		return fmt.Errorf("writing site: %w", err)
	}

	if !quiet {
		fmt.Fprintf(a.Stdout, "wrote %d files to %s\n", len(files), cfg.Output)
		for _, f := range files {
			fmt.Fprintf(a.Stdout, "  %s\n", filepath.ToSlash(f))
		}
	}
	return nil
}

// anchorTime picks the timestamp the site is rendered "as of": the newest
// history record, else the newest incident date, else the current time.
// Deriving it from the inputs keeps `build` a pure function.
func anchorTime(records []history.Record, incidents []incident.Incident, clock func() time.Time) time.Time {
	var anchor time.Time
	for _, r := range records {
		if r.TS.After(anchor) {
			anchor = r.TS
		}
	}
	if anchor.IsZero() {
		for _, in := range incidents {
			if in.Date.After(anchor) {
				anchor = in.Date
			}
		}
	}
	if anchor.IsZero() {
		anchor = clock()
	}
	return anchor.UTC()
}

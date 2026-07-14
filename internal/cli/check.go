package cli

import (
	"context"
	"fmt"

	"github.com/JaydenCJ/okpage/internal/history"
)

// cmdCheck probes every configured service once, appends the outcomes to the
// history file, prunes expired records, and optionally rebuilds the site.
// This is the command a cron job runs every few minutes.
func (a *App) cmdCheck(ctx context.Context, args []string) error {
	fs := newFlagSet("check")
	cfgPath := configFlag(fs)
	buildAfter := fs.Bool("build", false, "")
	quiet := fs.Bool("quiet", false, "")
	if err := fs.Parse(args); err != nil {
		return usageErr("check: %v", err)
	}
	if fs.NArg() > 0 {
		return usageErr("check: unexpected argument %q", fs.Arg(0))
	}

	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		return err
	}

	now := a.Now().UTC()
	results := a.Prober.Run(ctx, cfg.Services, cfg.Timeout)

	records := make([]history.Record, 0, len(results))
	up, down := 0, 0
	for _, res := range results {
		records = append(records, history.Record{
			TS:        now,
			Service:   res.Service,
			OK:        res.OK,
			LatencyMS: res.Latency.Milliseconds(),
			Detail:    res.Detail,
		})
		if res.OK {
			up++
		} else {
			down++
		}
		if !*quiet {
			if res.OK {
				fmt.Fprintf(a.Stdout, "   up  %-20s %5d ms  %s\n",
					res.Service, res.Latency.Milliseconds(), res.Detail)
			} else {
				fmt.Fprintf(a.Stdout, " DOWN  %-20s %8s  %s\n", res.Service, "", res.Detail)
			}
		}
	}

	if err := history.Append(cfg.History, records); err != nil {
		return fmt.Errorf("recording history: %w", err)
	}
	cutoff := now.AddDate(0, 0, -cfg.RetentionDays)
	if _, err := history.Prune(cfg.History, cutoff); err != nil {
		return fmt.Errorf("pruning history: %w", err)
	}

	if !*quiet {
		fmt.Fprintf(a.Stdout, "%d up, %d down\n", up, down)
	}

	if *buildAfter {
		if err := a.build(cfg, *quiet); err != nil {
			return err
		}
	}

	if down > 0 {
		noun := "services"
		if len(results) == 1 {
			noun = "service"
		}
		return downErr("%d of %d %s down", down, len(results), noun)
	}
	return nil
}

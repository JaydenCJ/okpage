// Package stats turns raw probe history into the numbers a status page
// shows: uptime percentages over rolling windows, per-day availability
// buckets for the bar strip, and the overall page state.
//
// Everything here is a pure function of (records, now) — no clocks, no IO —
// so identical input always renders an identical page.
package stats

import (
	"fmt"
	"time"

	"github.com/JaydenCJ/okpage/internal/history"
)

// Day states for the availability bar strip.
const (
	DayUp       = "up"       // every probe that day passed
	DayDegraded = "degraded" // some probes failed
	DayDown     = "down"     // every probe that day failed
	DayNoData   = "nodata"   // no probes recorded that day
)

// Overall page states.
const (
	OverallOperational = "operational" // every service's latest probe passed
	OverallDegraded    = "degraded"    // some latest probes failed
	OverallOutage      = "outage"      // every latest probe failed
	OverallUnknown     = "unknown"     // no history at all yet
)

// Window is an uptime percentage over a rolling time window.
type Window struct {
	Uptime  float64 // 0–100; meaningless when Samples == 0
	Samples int
}

// String renders "99.98%" or an em-dash when there is no data, exactly as
// shown on the page and in the terminal.
func (w Window) String() string {
	if w.Samples == 0 {
		return "—"
	}
	return fmt.Sprintf("%.2f%%", w.Uptime)
}

// Day is one bucket of the availability bar strip.
type Day struct {
	Date   time.Time // midnight UTC of the bucket
	State  string    // DayUp / DayDegraded / DayDown / DayNoData
	Total  int
	Failed int
}

// Service aggregates everything the page shows for one service.
type Service struct {
	Name        string
	HasData     bool
	LastOK      bool
	LastDetail  string
	LastLatency int64 // milliseconds
	LastChecked time.Time
	Uptime24h   Window
	Uptime7d    Window
	Uptime90d   Window
	Days        []Day // exactly `days` buckets, oldest first
}

// Compute aggregates records for each named service. Services keep their
// input order; records for unknown services are ignored (they may belong to
// a service that was renamed or removed from the config).
func Compute(records []history.Record, names []string, now time.Time, days int) []Service {
	byService := make(map[string][]history.Record, len(names))
	known := make(map[string]bool, len(names))
	for _, name := range names {
		known[name] = true
	}
	for _, rec := range records {
		if known[rec.Service] {
			byService[rec.Service] = append(byService[rec.Service], rec)
		}
	}

	out := make([]Service, 0, len(names))
	for _, name := range names {
		out = append(out, computeOne(name, byService[name], now, days))
	}
	return out
}

func computeOne(name string, recs []history.Record, now time.Time, days int) Service {
	svc := Service{Name: name}

	var last history.Record
	for _, rec := range recs {
		if !svc.HasData || rec.TS.After(last.TS) {
			last = rec
			svc.HasData = true
		}
	}
	if svc.HasData {
		svc.LastOK = last.OK
		svc.LastDetail = last.Detail
		svc.LastLatency = last.LatencyMS
		svc.LastChecked = last.TS
	}

	svc.Uptime24h = window(recs, now.Add(-24*time.Hour))
	svc.Uptime7d = window(recs, now.Add(-7*24*time.Hour))
	svc.Uptime90d = window(recs, now.Add(-90*24*time.Hour))
	svc.Days = buckets(recs, now, days)
	return svc
}

// window computes uptime over records with TS >= since.
func window(recs []history.Record, since time.Time) Window {
	var total, ok int
	for _, rec := range recs {
		if rec.TS.Before(since) {
			continue
		}
		total++
		if rec.OK {
			ok++
		}
	}
	if total == 0 {
		return Window{}
	}
	return Window{Uptime: 100 * float64(ok) / float64(total), Samples: total}
}

// buckets groups records into `days` UTC calendar days ending today,
// oldest first. Calendar days (not rolling 24h slices) match what every
// hosted status page renders and what humans expect a "day" to mean.
func buckets(recs []history.Record, now time.Time, days int) []Day {
	today := midnightUTC(now)
	first := today.AddDate(0, 0, -(days - 1))

	out := make([]Day, days)
	for i := range out {
		out[i] = Day{Date: first.AddDate(0, 0, i), State: DayNoData}
	}
	for _, rec := range recs {
		day := midnightUTC(rec.TS)
		idx := int(day.Sub(first).Hours() / 24)
		if idx < 0 || idx >= days {
			continue
		}
		out[idx].Total++
		if !rec.OK {
			out[idx].Failed++
		}
	}
	for i := range out {
		switch d := &out[i]; {
		case d.Total == 0:
			d.State = DayNoData
		case d.Failed == 0:
			d.State = DayUp
		case d.Failed == d.Total:
			d.State = DayDown
		default:
			d.State = DayDegraded
		}
	}
	return out
}

func midnightUTC(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// Overall derives the page-level state from the latest probe of every
// service. Services that have never been probed do not count against the
// page — a freshly added service should not flip the banner to "degraded".
func Overall(services []Service) string {
	var up, down int
	for _, svc := range services {
		if !svc.HasData {
			continue
		}
		if svc.LastOK {
			up++
		} else {
			down++
		}
	}
	switch {
	case up == 0 && down == 0:
		return OverallUnknown
	case down == 0:
		return OverallOperational
	case up == 0:
		return OverallOutage
	default:
		return OverallDegraded
	}
}

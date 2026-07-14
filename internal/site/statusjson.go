package site

// status.json is the machine-readable twin of index.html: same data, stable
// schema, safe to poll from dashboards, bots, or a second okpage watching
// the first. schema_version is bumped only on breaking changes.

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/JaydenCJ/okpage/internal/stats"
	"github.com/JaydenCJ/okpage/internal/version"
)

type statusDoc struct {
	Tool          string        `json:"tool"`
	Version       string        `json:"version"`
	SchemaVersion int           `json:"schema_version"`
	Title         string        `json:"title"`
	AsOf          time.Time     `json:"as_of"` // newest input timestamp, not build wall time
	Overall       string        `json:"overall"`
	Services      []serviceDoc  `json:"services"`
	Incidents     []incidentDoc `json:"incidents"`
}

type serviceDoc struct {
	Name        string     `json:"name"`
	State       string     `json:"state"` // "up", "down", or "unknown"
	Detail      string     `json:"detail,omitempty"`
	LatencyMS   *int64     `json:"latency_ms"`   // null until first probe
	LastChecked *time.Time `json:"last_checked"` // null until first probe
	Uptime      uptimeDoc  `json:"uptime"`
	Badge       string     `json:"badge"`
}

type uptimeDoc struct {
	// Percentages are null (not 0) when no samples exist in the window, so
	// consumers can tell "no data" apart from "hard down".
	Day24h *float64 `json:"24h"`
	Day7d  *float64 `json:"7d"`
	Day90d *float64 `json:"90d"`
}

type incidentDoc struct {
	Slug     string    `json:"slug"`
	Title    string    `json:"title"`
	Date     time.Time `json:"date"`
	Status   string    `json:"status"`
	Affected []string  `json:"affected"`
}

func renderStatusJSON(data Data) ([]byte, error) {
	doc := statusDoc{
		Tool:          "okpage",
		Version:       version.Version,
		SchemaVersion: 1,
		Title:         data.Title,
		AsOf:          data.Now.UTC(),
		Overall:       data.Overall,
		Services:      []serviceDoc{},
		Incidents:     []incidentDoc{},
	}

	for _, svc := range data.Services {
		sd := serviceDoc{
			Name:  svc.Name,
			State: "unknown",
			Badge: "badge/" + Slug(svc.Name) + ".svg",
			Uptime: uptimeDoc{
				Day24h: windowPct(svc.Uptime24h),
				Day7d:  windowPct(svc.Uptime7d),
				Day90d: windowPct(svc.Uptime90d),
			},
		}
		if svc.HasData {
			if svc.LastOK {
				sd.State = "up"
			} else {
				sd.State = "down"
				sd.Detail = svc.LastDetail
			}
			latency := svc.LastLatency
			checked := svc.LastChecked.UTC()
			sd.LatencyMS = &latency
			sd.LastChecked = &checked
		}
		doc.Services = append(doc.Services, sd)
	}

	for _, in := range data.Incidents {
		affected := in.Affected
		if affected == nil {
			affected = []string{}
		}
		doc.Incidents = append(doc.Incidents, incidentDoc{
			Slug:     in.Slug,
			Title:    in.Title,
			Date:     in.Date.UTC(),
			Status:   in.Status,
			Affected: affected,
		})
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func windowPct(w stats.Window) *float64 {
	if w.Samples == 0 {
		return nil
	}
	// Round to two decimals so the JSON matches the page exactly.
	pct := float64(int(w.Uptime*100+0.5)) / 100
	return &pct
}

// Package site renders the static output: index.html, status.json, and one
// SVG badge per service. Everything is written into a single directory that
// can be pushed to any static host — no server-side code, no assets fetched
// from third parties, no JavaScript.
package site

import (
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/JaydenCJ/okpage/internal/incident"
	"github.com/JaydenCJ/okpage/internal/stats"
	"github.com/JaydenCJ/okpage/internal/version"
)

// Data is everything a page render needs; it is assembled by the CLI and
// contains no live handles, so renders are pure and repeatable.
type Data struct {
	Title     string
	Now       time.Time
	Overall   string // stats.Overall* constant
	Services  []stats.Service
	Incidents []incident.Incident
}

// Build writes the complete static site into dir, creating it if needed.
// It returns the list of files written (relative to dir), for logging.
func Build(dir string, data Data) ([]string, error) {
	if err := os.MkdirAll(filepath.Join(dir, "badge"), 0o755); err != nil {
		return nil, err
	}

	var files []string
	write := func(rel string, content []byte) error {
		if err := os.WriteFile(filepath.Join(dir, rel), content, 0o644); err != nil {
			return err
		}
		files = append(files, rel)
		return nil
	}

	page, err := renderHTML(data)
	if err != nil {
		return nil, err
	}
	if err := write("index.html", page); err != nil {
		return nil, err
	}

	statusJSON, err := renderStatusJSON(data)
	if err != nil {
		return nil, err
	}
	if err := write("status.json", statusJSON); err != nil {
		return nil, err
	}

	for _, svc := range data.Services {
		rel := filepath.Join("badge", Slug(svc.Name)+".svg")
		if err := write(rel, renderBadge(svc)); err != nil {
			return nil, err
		}
	}
	return files, nil
}

// Slug converts a service name into a stable, URL-safe file name:
// lowercase, runs of non-alphanumerics collapsed to single hyphens.
func Slug(name string) string {
	var b strings.Builder
	pendingHyphen := false
	for _, r := range strings.ToLower(name) {
		isAlnum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlnum {
			if pendingHyphen && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingHyphen = false
			b.WriteRune(r)
		} else {
			pendingHyphen = true
		}
	}
	if b.Len() == 0 {
		return "service"
	}
	return b.String()
}

// overallText maps the overall state to the banner headline.
func overallText(overall string) string {
	switch overall {
	case stats.OverallOperational:
		return "All systems operational"
	case stats.OverallDegraded:
		return "Partial outage"
	case stats.OverallOutage:
		return "Major outage"
	default:
		return "No data yet"
	}
}

// renderHTML executes the embedded page template.
func renderHTML(data Data) ([]byte, error) {
	tmpl := template.Must(template.New("page").Funcs(template.FuncMap{
		"overallText": overallText,
		"slug":        Slug,
		"utc": func(t time.Time) string {
			return t.UTC().Format("2006-01-02 15:04 UTC")
		},
		"day": func(t time.Time) string {
			return t.UTC().Format("2006-01-02")
		},
		"dayTitle": func(d stats.Day) string {
			date := d.Date.Format("2006-01-02")
			if d.Total == 0 {
				return date + ": no data"
			}
			noun := "checks"
			if d.Total == 1 {
				noun = "check"
			}
			return fmt.Sprintf("%s: %d/%d %s passed", date, d.Total-d.Failed, d.Total, noun)
		},
		"join":    func(items []string) string { return strings.Join(items, ", ") },
		"html":    func(s string) template.HTML { return template.HTML(s) }, //nolint — incident HTML is produced by our sanitizing renderer
		"version": func() string { return version.Version },
	}).Parse(pageTemplate))

	var b strings.Builder
	if err := tmpl.Execute(&b, data); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}

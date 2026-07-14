// Package incident reads incidents from markdown files with a small YAML-ish
// front matter block. Incidents are files, not database rows: they diff in
// git, survive any migration, and can be written from a phone over SSH
// during an outage.
//
// File shape:
//
//	---
//	title: Elevated API latency
//	date: 2026-07-10T14:30:00Z
//	status: resolved
//	affected: [API, Website]
//	---
//
//	Markdown body…
package incident

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Valid incident statuses, in escalation-to-resolution order.
var validStatuses = []string{"investigating", "identified", "monitoring", "resolved"}

// Incident is one parsed incident file.
type Incident struct {
	Slug     string // filename without extension; stable anchor on the page
	Title    string
	Date     time.Time
	Status   string
	Affected []string // service names, informational only
	Body     string   // raw markdown body
	BodyHTML string   // rendered by the markdown subset in this package
}

// Resolved reports whether the incident no longer affects the page banner.
func (in Incident) Resolved() bool { return in.Status == "resolved" }

// LoadDir parses every *.md file in dir, newest first (ties broken by slug
// for a fully deterministic page). A missing directory means no incidents.
func LoadDir(dir string) ([]Incident, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var incidents []Incident
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".md") || strings.HasPrefix(name, ".") {
			continue
		}
		// README.md in the incidents dir documents the format; skip it.
		if strings.EqualFold(name, "README.md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		in, err := Parse(name, string(data))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Join(dir, name), err)
		}
		incidents = append(incidents, in)
	}

	sort.Slice(incidents, func(i, j int) bool {
		if !incidents[i].Date.Equal(incidents[j].Date) {
			return incidents[i].Date.After(incidents[j].Date)
		}
		return incidents[i].Slug < incidents[j].Slug
	})
	return incidents, nil
}

// Parse parses one incident file. name is the file's base name and only
// contributes the slug.
func Parse(name, data string) (Incident, error) {
	in := Incident{Slug: strings.TrimSuffix(filepath.Base(name), ".md")}

	front, body, err := splitFrontMatter(data)
	if err != nil {
		return in, err
	}
	fields, err := parseFrontMatter(front)
	if err != nil {
		return in, err
	}

	in.Title = fields["title"]
	if in.Title == "" {
		return in, fmt.Errorf("front matter is missing \"title\"")
	}

	in.Date, err = parseDate(fields["date"])
	if err != nil {
		return in, err
	}

	in.Status = fields["status"]
	if !validStatus(in.Status) {
		return in, fmt.Errorf("invalid status %q (want one of %s)",
			in.Status, strings.Join(validStatuses, ", "))
	}

	in.Affected = parseList(fields["affected"])

	in.Body = strings.TrimSpace(body)
	in.BodyHTML = RenderMarkdown(in.Body)
	return in, nil
}

// splitFrontMatter separates the leading `--- … ---` block from the body.
func splitFrontMatter(data string) (front, body string, err error) {
	normalized := strings.ReplaceAll(data, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return "", "", fmt.Errorf("incident files must start with a --- front matter block")
	}
	rest := normalized[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", "", fmt.Errorf("unterminated front matter (missing closing ---)")
	}
	front = rest[:end]
	body = rest[end+len("\n---"):]
	body = strings.TrimPrefix(body, "\n")
	return front, body, nil
}

// parseFrontMatter reads `key: value` lines. Unknown keys are rejected so a
// typo like `affcted:` cannot silently drop information from the page.
func parseFrontMatter(front string) (map[string]string, error) {
	known := map[string]bool{"title": true, "date": true, "status": true, "affected": true}
	fields := map[string]string{}
	for i, line := range strings.Split(front, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		colon := strings.Index(line, ":")
		if colon < 0 {
			return nil, fmt.Errorf("front matter line %d: expected key: value, got %q", i+1, line)
		}
		key := strings.TrimSpace(line[:colon])
		if !known[key] {
			return nil, fmt.Errorf("front matter line %d: unknown key %q", i+1, key)
		}
		if _, dup := fields[key]; dup {
			return nil, fmt.Errorf("front matter line %d: key %q is set twice", i+1, key)
		}
		fields[key] = strings.TrimSpace(line[colon+1:])
	}
	return fields, nil
}

// parseDate accepts RFC 3339 or a bare YYYY-MM-DD (treated as midnight UTC).
func parseDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("front matter is missing \"date\"")
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid date %q (want RFC 3339 or YYYY-MM-DD)", s)
}

// parseList accepts `[A, B]` or a bare comma-separated list.
func parseList(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func validStatus(s string) bool {
	for _, v := range validStatuses {
		if s == v {
			return true
		}
	}
	return false
}

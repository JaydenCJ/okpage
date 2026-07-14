// Tests for incident front matter parsing and directory loading.
package incident

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const sample = `---
title: Elevated API latency
date: 2026-07-10T14:30:00Z
status: resolved
affected: [API, Website]
---

Latency rose above 2s for around 20 minutes.

## Root cause

A runaway backup job saturated disk IO.
`

func TestParseFullIncident(t *testing.T) {
	in, err := Parse("2026-07-10-elevated-api-latency.md", sample)
	if err != nil {
		t.Fatal(err)
	}
	if in.Slug != "2026-07-10-elevated-api-latency" {
		t.Errorf("Slug = %q", in.Slug)
	}
	if in.Title != "Elevated API latency" {
		t.Errorf("Title = %q", in.Title)
	}
	if !in.Date.Equal(time.Date(2026, 7, 10, 14, 30, 0, 0, time.UTC)) {
		t.Errorf("Date = %v", in.Date)
	}
	if in.Status != "resolved" || !in.Resolved() {
		t.Errorf("Status = %q", in.Status)
	}
	if !reflect.DeepEqual(in.Affected, []string{"API", "Website"}) {
		t.Errorf("Affected = %v", in.Affected)
	}
	if !strings.Contains(in.BodyHTML, "<h4>Root cause</h4>") {
		t.Errorf("body not rendered: %q", in.BodyHTML)
	}
}

func TestParseAcceptsAlternateFieldForms(t *testing.T) {
	// Bare YYYY-MM-DD dates parse as midnight UTC.
	in, err := Parse("x.md", "---\ntitle: T\ndate: 2026-07-01\nstatus: monitoring\n---\nbody")
	if err != nil {
		t.Fatal(err)
	}
	if !in.Date.Equal(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("Date = %v, want midnight UTC", in.Date)
	}
	if in.Resolved() {
		t.Error("monitoring is not resolved")
	}

	// affected accepts a bare comma list as well as [brackets].
	in, err = Parse("x.md", "---\ntitle: T\ndate: 2026-07-01\nstatus: identified\naffected: API, Website\n---\n")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in.Affected, []string{"API", "Website"}) {
		t.Errorf("Affected = %v", in.Affected)
	}

	// An explicit empty list means "nothing affected", not an error.
	in, err = Parse("x.md", "---\ntitle: T\ndate: 2026-07-01\nstatus: resolved\naffected: []\n---\n")
	if err != nil {
		t.Fatal(err)
	}
	if in.Affected != nil {
		t.Errorf("Affected = %v, want nil", in.Affected)
	}
}

func TestParseHandlesCRLF(t *testing.T) {
	crlf := strings.ReplaceAll(sample, "\n", "\r\n")
	in, err := Parse("x.md", crlf)
	if err != nil {
		t.Fatalf("CRLF files (edited on Windows) must parse: %v", err)
	}
	if in.Title != "Elevated API latency" {
		t.Errorf("Title = %q", in.Title)
	}
}

// TestParseRejectsBrokenFrontMatter covers every parse rejection. Incidents
// are written by hand mid-outage, so mistakes are certain; each one must
// fail loudly with a message naming the problem.
func TestParseRejectsBrokenFrontMatter(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"no front matter", "Just a body, no front matter.", "front matter"},
		{"unterminated block", "---\ntitle: T\ndate: 2026-07-01\nstatus: resolved\n", "unterminated front matter"},
		{"missing title", "---\ndate: 2026-07-01\nstatus: resolved\n---\n", `missing "title"`},
		{"missing date", "---\ntitle: T\nstatus: resolved\n---\n", `missing "date"`},
		{"unparseable date", "---\ntitle: T\ndate: July 1st\nstatus: resolved\n---\n", "invalid date"},
		{"invented status", "---\ntitle: T\ndate: 2026-07-01\nstatus: fixed\n---\n", `invalid status "fixed"`},
		{"typo in key", "---\ntitle: T\ndate: 2026-07-01\nstatus: resolved\naffcted: [API]\n---\n", `unknown key "affcted"`},
		{"duplicate key", "---\ntitle: A\ntitle: B\ndate: 2026-07-01\nstatus: resolved\n---\n", "set twice"},
		{"line without colon", "---\ntitle: T\njust words\ndate: 2026-07-01\nstatus: resolved\n---\n", "key: value"},
	}
	for _, tc := range cases {
		_, err := Parse("x.md", tc.src)
		if err == nil {
			t.Errorf("%s: expected error containing %q, got nil", tc.name, tc.want)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %q does not contain %q", tc.name, err, tc.want)
		}
	}
}

func writeIncident(t *testing.T, dir, name, title, date string) {
	t.Helper()
	content := "---\ntitle: " + title + "\ndate: " + date + "\nstatus: resolved\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDirSortsNewestFirst(t *testing.T) {
	dir := t.TempDir()
	writeIncident(t, dir, "old.md", "Old", "2026-06-01")
	writeIncident(t, dir, "new.md", "New", "2026-07-01")
	writeIncident(t, dir, "mid.md", "Mid", "2026-06-15")

	incidents, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, in := range incidents {
		titles = append(titles, in.Title)
	}
	if !reflect.DeepEqual(titles, []string{"New", "Mid", "Old"}) {
		t.Fatalf("order = %v", titles)
	}
}

func TestLoadDirBreaksDateTiesBySlug(t *testing.T) {
	dir := t.TempDir()
	writeIncident(t, dir, "b-second.md", "B", "2026-07-01")
	writeIncident(t, dir, "a-first.md", "A", "2026-07-01")

	incidents, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if incidents[0].Slug != "a-first" || incidents[1].Slug != "b-second" {
		t.Fatalf("tie-break not deterministic: %v, %v", incidents[0].Slug, incidents[1].Slug)
	}
}

func TestLoadDirMissingDirectoryMeansNoIncidents(t *testing.T) {
	incidents, err := LoadDir(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("missing incidents dir must not be an error: %v", err)
	}
	if incidents != nil {
		t.Fatalf("got %v, want nil", incidents)
	}
}

func TestLoadDirSkipsNonMarkdownAndReadme(t *testing.T) {
	dir := t.TempDir()
	writeIncident(t, dir, "real.md", "Real", "2026-07-01")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("format docs, no front matter"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("scratch"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".draft.md"), []byte("hidden"), 0o644); err != nil {
		t.Fatal(err)
	}

	incidents, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(incidents) != 1 || incidents[0].Title != "Real" {
		t.Fatalf("got %+v, want only the real incident", incidents)
	}
}

func TestLoadDirNamesTheBrokenFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.md"), []byte("no front matter"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadDir(dir)
	if err == nil || !strings.Contains(err.Error(), "broken.md") {
		t.Fatalf("error should name the file, got %v", err)
	}
}

// Tests for the static-site renderer: file layout, HTML content, the
// status.json schema, badges, and byte-for-byte determinism.
package site

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JaydenCJ/okpage/internal/incident"
	"github.com/JaydenCJ/okpage/internal/stats"
)

var fixedNow = time.Date(2026, 7, 12, 8, 30, 0, 0, time.UTC)

func demoData() Data {
	up := stats.Service{
		Name: "Web Site", HasData: true, LastOK: true, LastLatency: 42,
		LastChecked: fixedNow.Add(-5 * time.Minute),
		Uptime24h:   stats.Window{Uptime: 100, Samples: 24},
		Uptime7d:    stats.Window{Uptime: 99.4, Samples: 168},
		Uptime90d:   stats.Window{Uptime: 99.9, Samples: 2000},
		Days: []stats.Day{
			{Date: fixedNow.AddDate(0, 0, -2), State: stats.DayUp, Total: 24},
			{Date: fixedNow.AddDate(0, 0, -1), State: stats.DayDegraded, Total: 24, Failed: 3},
			{Date: fixedNow, State: stats.DayUp, Total: 9},
		},
	}
	down := stats.Service{
		Name: "Postgres", HasData: true, LastOK: false,
		LastDetail:  "connection refused",
		LastChecked: fixedNow.Add(-5 * time.Minute),
		Uptime24h:   stats.Window{Uptime: 80, Samples: 20},
		Days: []stats.Day{
			{Date: fixedNow, State: stats.DayDown, Total: 4, Failed: 4},
		},
	}
	fresh := stats.Service{Name: "New Thing", Days: []stats.Day{{Date: fixedNow, State: stats.DayNoData}}}

	inc, err := incident.Parse("2026-07-10-db-outage.md", `---
title: Database outage
date: 2026-07-10T14:00:00Z
status: monitoring
affected: [Postgres]
---

Failover to the replica is **in progress**.
`)
	if err != nil {
		panic(err)
	}

	return Data{
		Title:     "Acme Status",
		Now:       fixedNow,
		Overall:   stats.OverallDegraded,
		Services:  []stats.Service{up, down, fresh},
		Incidents: []incident.Incident{inc},
	}
}

func buildDemo(t *testing.T) (string, []string) {
	t.Helper()
	dir := t.TempDir()
	files, err := Build(dir, demoData())
	if err != nil {
		t.Fatal(err)
	}
	return dir, files
}

func read(t *testing.T, dir, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestBuildWritesExpectedFiles(t *testing.T) {
	dir, files := buildDemo(t)
	want := []string{
		"index.html", "status.json",
		filepath.Join("badge", "web-site.svg"),
		filepath.Join("badge", "postgres.svg"),
		filepath.Join("badge", "new-thing.svg"),
	}
	if len(files) != len(want) {
		t.Fatalf("files = %v", files)
	}
	for _, rel := range want {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("missing %s", rel)
		}
	}
}

func TestIndexHTMLShowsServicesStatesAndUptime(t *testing.T) {
	dir, _ := buildDemo(t)
	page := read(t, dir, "index.html")

	for _, want := range []string{
		"<title>Acme Status</title>",
		"Partial outage",
		"Web Site", "Postgres", "New Thing",
		"connection refused", // failure detail is shown
		"42 ms",
		"no data", // fresh service
		"status.json",
		"99.40%", // 7d uptime
		`class="degraded" title="2026-07-11: 21/24 checks passed"`, // day bar tooltip
	} {
		if !strings.Contains(page, want) {
			t.Errorf("index.html missing %q", want)
		}
	}
}

func TestIndexHTMLRendersIncidents(t *testing.T) {
	dir, _ := buildDemo(t)
	page := read(t, dir, "index.html")
	for _, want := range []string{
		"Database outage",
		`class="pill monitoring"`,
		"affects Postgres",
		"<strong>in progress</strong>",
		`id="incident-2026-07-10-db-outage"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("incidents section missing %q", want)
		}
	}
}

func TestIndexHTMLEscapesHostileTitles(t *testing.T) {
	data := demoData()
	data.Title = `<script>alert("x")</script>`
	dir := t.TempDir()
	if _, err := Build(dir, data); err != nil {
		t.Fatal(err)
	}
	page := read(t, dir, "index.html")
	if strings.Contains(page, `<script>alert`) {
		t.Fatal("title not escaped")
	}
}

func TestIndexHTMLIsSelfContained(t *testing.T) {
	// The page must work from file:// with the network cable pulled: no
	// external scripts, stylesheets, fonts, or images.
	dir, _ := buildDemo(t)
	page := read(t, dir, "index.html")
	for _, banned := range []string{"<script", "src=\"http", "link rel=\"stylesheet\" href=\"http", "@import", "url(http"} {
		if strings.Contains(page, banned) {
			t.Errorf("page is not self-contained: found %q", banned)
		}
	}
}

func TestStatusJSONSchema(t *testing.T) {
	dir, _ := buildDemo(t)
	var doc map[string]any
	if err := json.Unmarshal([]byte(read(t, dir, "status.json")), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["tool"] != "okpage" || doc["schema_version"] != float64(1) {
		t.Errorf("envelope wrong: %v %v", doc["tool"], doc["schema_version"])
	}
	if doc["overall"] != "degraded" {
		t.Errorf("overall = %v", doc["overall"])
	}
	services := doc["services"].([]any)
	if len(services) != 3 {
		t.Fatalf("services = %d, want 3", len(services))
	}

	first := services[0].(map[string]any)
	if first["name"] != "Web Site" || first["state"] != "up" {
		t.Errorf("first service: %v", first)
	}
	if first["badge"] != "badge/web-site.svg" {
		t.Errorf("badge path: %v", first["badge"])
	}
	uptime := first["uptime"].(map[string]any)
	if uptime["7d"] != 99.4 {
		t.Errorf("uptime 7d = %v", uptime["7d"])
	}

	down := services[1].(map[string]any)
	if down["state"] != "down" || down["detail"] != "connection refused" {
		t.Errorf("down service: %v", down)
	}

	fresh := services[2].(map[string]any)
	if fresh["state"] != "unknown" {
		t.Errorf("fresh state = %v", fresh["state"])
	}
	if fresh["latency_ms"] != nil || fresh["last_checked"] != nil {
		t.Errorf("fresh service must have null latency/checked: %v", fresh)
	}
	if fresh["uptime"].(map[string]any)["24h"] != nil {
		t.Errorf("no-sample uptime must be null, got %v", fresh["uptime"])
	}
}

func TestStatusJSONIncidents(t *testing.T) {
	dir, _ := buildDemo(t)
	var doc struct {
		Incidents []struct {
			Slug     string   `json:"slug"`
			Status   string   `json:"status"`
			Affected []string `json:"affected"`
		} `json:"incidents"`
	}
	if err := json.Unmarshal([]byte(read(t, dir, "status.json")), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Incidents) != 1 {
		t.Fatalf("incidents = %d", len(doc.Incidents))
	}
	if doc.Incidents[0].Slug != "2026-07-10-db-outage" || doc.Incidents[0].Status != "monitoring" {
		t.Errorf("incident = %+v", doc.Incidents[0])
	}
	if len(doc.Incidents[0].Affected) != 1 || doc.Incidents[0].Affected[0] != "Postgres" {
		t.Errorf("affected = %v", doc.Incidents[0].Affected)
	}
}

func TestBadgesReflectServiceState(t *testing.T) {
	dir, _ := buildDemo(t)

	up := read(t, dir, filepath.Join("badge", "web-site.svg"))
	if !strings.Contains(up, ">up</text>") || !strings.Contains(up, "#4c1") {
		t.Errorf("up badge wrong: %s", up)
	}
	down := read(t, dir, filepath.Join("badge", "postgres.svg"))
	if !strings.Contains(down, ">down</text>") || !strings.Contains(down, "#e05d44") {
		t.Errorf("down badge wrong: %s", down)
	}
	fresh := read(t, dir, filepath.Join("badge", "new-thing.svg"))
	if !strings.Contains(fresh, ">no data</text>") {
		t.Errorf("no-data badge wrong: %s", fresh)
	}

	// Hostile service names must be escaped inside the SVG.
	svg := string(renderBadge(stats.Service{Name: `a<b>&"c`, HasData: true, LastOK: true}))
	if strings.Contains(svg, "<b>") || !strings.Contains(svg, "a&lt;b&gt;&amp;&#34;c") {
		t.Errorf("name not escaped: %s", svg)
	}
}

func TestBuildIsByteForByteDeterministic(t *testing.T) {
	dirA, _ := buildDemo(t)
	dirB := t.TempDir()
	if _, err := Build(dirB, demoData()); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"index.html", "status.json", filepath.Join("badge", "postgres.svg")} {
		if read(t, dirA, rel) != read(t, dirB, rel) {
			t.Errorf("%s differs between identical builds", rel)
		}
	}
}

func TestBuildOverwritesPreviousOutput(t *testing.T) {
	dir, _ := buildDemo(t)
	data := demoData()
	data.Title = "Renamed"
	if _, err := Build(dir, data); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, dir, "index.html"), "Renamed") {
		t.Fatal("rebuild did not overwrite index.html")
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"API":               "api",
		"Web Site":          "web-site",
		"a  b--c":           "a-b-c",
		"  padded  ":        "padded",
		"データベース":            "service", // non-ASCII collapses to fallback
		"v2.0 (EU/West) #1": "v2-0-eu-west-1",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

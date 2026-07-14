// Tests for the aggregation layer. All inputs use pinned timestamps so
// every percentage and bucket is exact.
package stats

import (
	"testing"
	"time"

	"github.com/JaydenCJ/okpage/internal/history"
)

// now is fixed mid-afternoon UTC so day-boundary math is unambiguous.
var now = time.Date(2026, 7, 10, 15, 0, 0, 0, time.UTC)

func rec(daysAgo int, hour int, service string, ok bool) history.Record {
	t := now.AddDate(0, 0, -daysAgo)
	t = time.Date(t.Year(), t.Month(), t.Day(), hour, 0, 0, 0, time.UTC)
	return history.Record{TS: t, Service: service, OK: ok, LatencyMS: 20, Detail: "200"}
}

func one(t *testing.T, recs []history.Record, days int) Service {
	t.Helper()
	out := Compute(recs, []string{"web"}, now, days)
	if len(out) != 1 {
		t.Fatalf("got %d services, want 1", len(out))
	}
	return out[0]
}

func TestComputeUptimeWindows(t *testing.T) {
	recs := []history.Record{
		rec(0, 10, "web", true),  // inside 24h
		rec(0, 12, "web", false), // inside 24h
		rec(3, 10, "web", true),  // inside 7d only
		rec(30, 10, "web", true), // inside 90d only
	}
	svc := one(t, recs, 90)

	if svc.Uptime24h.Samples != 2 || svc.Uptime24h.Uptime != 50 {
		t.Errorf("24h = %+v, want 50%% of 2", svc.Uptime24h)
	}
	if svc.Uptime7d.Samples != 3 {
		t.Errorf("7d samples = %d, want 3", svc.Uptime7d.Samples)
	}
	if svc.Uptime90d.Samples != 4 || svc.Uptime90d.Uptime != 75 {
		t.Errorf("90d = %+v, want 75%% of 4", svc.Uptime90d)
	}
	if got := svc.Uptime24h.String(); got != "50.00%" {
		t.Errorf("24h renders %q, want 50.00%%", got)
	}
}

func TestComputeLastStateUsesNewestRecord(t *testing.T) {
	// Records arrive out of order (e.g. two cron hosts merging histories);
	// the newest timestamp must win, not the last line in the file.
	recs := []history.Record{
		rec(0, 14, "web", false),
		rec(0, 9, "web", true),
	}
	recs[0].Detail = "status 503 (want 2xx)"
	svc := one(t, recs, 30)

	if svc.LastOK {
		t.Fatal("newest record is a failure; LastOK must be false")
	}
	if svc.LastDetail != "status 503 (want 2xx)" {
		t.Errorf("LastDetail = %q", svc.LastDetail)
	}
	if !svc.LastChecked.Equal(recs[0].TS) {
		t.Errorf("LastChecked = %v, want %v", svc.LastChecked, recs[0].TS)
	}
}

func TestComputeServiceWithNoHistory(t *testing.T) {
	svc := one(t, nil, 30)
	if svc.HasData {
		t.Fatal("no records should mean HasData == false")
	}
	if svc.Uptime24h.Samples != 0 || svc.Uptime90d.Samples != 0 {
		t.Errorf("windows should be empty: %+v", svc)
	}
	if got := svc.Uptime24h.String(); got != "—" {
		t.Errorf("empty window renders %q, want an em-dash", got)
	}
}

func TestComputeIgnoresRecordsForUnknownServices(t *testing.T) {
	recs := []history.Record{
		rec(0, 10, "web", true),
		rec(0, 10, "removed-service", false),
	}
	svc := one(t, recs, 30)
	if svc.Uptime24h.Samples != 1 {
		t.Fatalf("samples = %d — records for removed services must not leak in", svc.Uptime24h.Samples)
	}
}

func TestComputeKeepsServiceInputOrder(t *testing.T) {
	out := Compute(nil, []string{"zeta", "alpha", "mid"}, now, 7)
	if out[0].Name != "zeta" || out[1].Name != "alpha" || out[2].Name != "mid" {
		t.Fatalf("order changed: %v", []string{out[0].Name, out[1].Name, out[2].Name})
	}
}

func TestBucketsCoverExactlyRequestedDaysOldestFirst(t *testing.T) {
	svc := one(t, nil, 14)
	if len(svc.Days) != 14 {
		t.Fatalf("got %d buckets, want 14", len(svc.Days))
	}
	first := svc.Days[0].Date
	last := svc.Days[len(svc.Days)-1].Date
	if !last.Equal(time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("last bucket = %v, want today UTC", last)
	}
	if !first.Equal(time.Date(2026, 6, 27, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("first bucket = %v, want 13 days before today", first)
	}
}

func TestBucketDayStates(t *testing.T) {
	recs := []history.Record{
		// today: all pass -> up
		rec(0, 9, "web", true), rec(0, 10, "web", true),
		// yesterday: mixed -> degraded
		rec(1, 9, "web", true), rec(1, 10, "web", false),
		// two days ago: all fail -> down
		rec(2, 9, "web", false), rec(2, 10, "web", false),
		// three days ago: nothing -> nodata
	}
	svc := one(t, recs, 4)
	states := []string{svc.Days[0].State, svc.Days[1].State, svc.Days[2].State, svc.Days[3].State}
	want := []string{DayNoData, DayDown, DayDegraded, DayUp}
	for i := range want {
		if states[i] != want[i] {
			t.Errorf("day %d = %q, want %q (all: %v)", i, states[i], want[i], states)
		}
	}
}

func TestBucketsUseUTCCalendarDays(t *testing.T) {
	// A record at 23:59 UTC belongs to that UTC day even if local time
	// elsewhere has rolled over. Feed a non-UTC timestamp and confirm it
	// lands in the right bucket.
	loc := time.FixedZone("UTC+9", 9*3600)
	inJST := time.Date(2026, 7, 10, 8, 30, 0, 0, loc) // 2026-07-09 23:30 UTC
	recs := []history.Record{{TS: inJST, Service: "web", OK: true}}
	svc := one(t, recs, 3)

	if svc.Days[1].Total != 1 { // 2026-07-09 bucket
		t.Fatalf("record placed in wrong bucket: %+v", svc.Days)
	}
	if svc.Days[2].Total != 0 {
		t.Fatalf("today should be empty: %+v", svc.Days[2])
	}
}

func TestBucketsIgnoreRecordsOlderThanTheStrip(t *testing.T) {
	recs := []history.Record{rec(10, 9, "web", false)}
	svc := one(t, recs, 3)
	for _, d := range svc.Days {
		if d.Total != 0 {
			t.Fatalf("ancient record leaked into %v", d)
		}
	}
}

func TestOverallStates(t *testing.T) {
	up := Service{Name: "a", HasData: true, LastOK: true}
	down := Service{Name: "b", HasData: true, LastOK: false}
	fresh := Service{Name: "c"} // never probed

	cases := []struct {
		name string
		in   []Service
		want string
	}{
		{"all up", []Service{up, up}, OverallOperational},
		{"mixed", []Service{up, down}, OverallDegraded},
		{"all down", []Service{down, down}, OverallOutage},
		{"nothing probed", []Service{fresh, fresh}, OverallUnknown},
		{"no services", nil, OverallUnknown},
		{"fresh service does not degrade", []Service{up, fresh}, OverallOperational},
	}
	for _, tc := range cases {
		if got := Overall(tc.in); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

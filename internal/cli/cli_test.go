// In-process integration tests for the CLI: every subcommand end-to-end
// against loopback servers and temp directories, with a pinned clock.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JaydenCJ/okpage/internal/history"
	"github.com/JaydenCJ/okpage/internal/probe"
)

var fixedNow = time.Date(2026, 7, 12, 9, 0, 0, 0, time.UTC)

// run executes the CLI in-process and returns exit code, stdout, stderr.
func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errBuf bytes.Buffer
	app := &App{
		Stdout: &out,
		Stderr: &errBuf,
		Prober: probe.New(),
		Now:    func() time.Time { return fixedNow },
	}
	code := app.Run(context.Background(), args)
	return code, out.String(), errBuf.String()
}

// healthyServer starts a loopback HTTP server answering 200 "ok".
func healthyServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	t.Cleanup(srv.Close)
	return srv
}

// writeConfig writes a config probing the given URLs into dir and returns
// the config path.
func writeConfig(t *testing.T, dir string, services ...string) string {
	t.Helper()
	cfg := "title = \"Test Status\"\ntimeout = \"5s\"\n" + strings.Join(services, "\n")
	path := filepath.Join(dir, "okpage.toml")
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func httpService(name, url string) string {
	return fmt.Sprintf("[[service]]\nname = %q\ntype = \"http\"\nurl = %q\n", name, url)
}

func TestVersionAndHelpCommands(t *testing.T) {
	for _, invocation := range [][]string{{"version"}, {"--version"}, {"-v"}} {
		code, out, _ := run(t, invocation...)
		if code != ExitOK {
			t.Errorf("%v: exit %d", invocation, code)
		}
		if out != "okpage 0.1.0\n" {
			t.Errorf("%v: out = %q", invocation, out)
		}
	}

	code, out, _ := run(t, "help")
	if code != ExitOK || !strings.Contains(out, "okpage 0.1.0") {
		t.Fatalf("help: exit = %d, out = %q", code, out)
	}
}

// TestUsageErrorsExit2 covers every argument mistake; each must exit 2 with
// a message that names the problem, never a stack trace.
func TestUsageErrorsExit2(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no arguments", nil, "Usage:"},
		{"unknown command", []string{"deploy"}, `unknown command "deploy"`},
		{"unknown flag", []string{"check", "--frobnicate"}, "check:"},
		{"stray positional", []string{"check", "extra"}, "unexpected argument"},
		{"stray build positional", []string{"build", "extra"}, "unexpected argument"},
		{"too many init dirs", []string{"init", "a", "b"}, "at most one"},
	}
	for _, tc := range cases {
		code, _, errOut := run(t, tc.args...)
		if code != ExitUsage {
			t.Errorf("%s: exit = %d, want %d", tc.name, code, ExitUsage)
		}
		if !strings.Contains(errOut, tc.want) {
			t.Errorf("%s: stderr %q does not contain %q", tc.name, errOut, tc.want)
		}
	}
}

func TestInitScaffoldsWorkingSetup(t *testing.T) {
	dir := t.TempDir()
	code, out, _ := run(t, "init", dir)
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out, "okpage.toml") {
		t.Errorf("out = %q", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "okpage.toml")); err != nil {
		t.Fatal("config not created")
	}
	if _, err := os.Stat(filepath.Join(dir, "incidents", "2026-07-12-welcome.md")); err != nil {
		t.Fatal("welcome incident not created (or not dated from the injected clock)")
	}

	// The scaffold must itself be valid: `build` on a fresh init succeeds.
	code, _, errOut := run(t, "build", "-c", filepath.Join(dir, "okpage.toml"))
	if code != ExitOK {
		t.Fatalf("build on fresh init failed: %s", errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "public", "index.html")); err != nil {
		t.Fatal("site not rendered from scaffold")
	}
}

func TestInitRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	if code, _, _ := run(t, "init", dir); code != ExitOK {
		t.Fatal("first init failed")
	}
	code, _, errOut := run(t, "init", dir)
	if code != ExitUsage || !strings.Contains(errOut, "not overwriting") {
		t.Fatalf("exit = %d, stderr = %q", code, errOut)
	}
}

func TestCheckAllUpExitsZeroAndRecordsHistory(t *testing.T) {
	srv := healthyServer(t)
	dir := t.TempDir()
	cfg := writeConfig(t, dir, httpService("Website", srv.URL))

	code, out, _ := run(t, "check", "-c", cfg)
	if code != ExitOK {
		t.Fatalf("exit = %d, out = %q", code, out)
	}
	if !strings.Contains(out, "Website") || !strings.Contains(out, "1 up, 0 down") {
		t.Errorf("out = %q", out)
	}

	recs, err := history.Load(filepath.Join(dir, "history.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || !recs[0].OK || recs[0].Service != "Website" {
		t.Fatalf("history = %+v", recs)
	}
	if !recs[0].TS.Equal(fixedNow) {
		t.Errorf("record TS = %v, want the injected clock", recs[0].TS)
	}

	// --quiet suppresses the table entirely (for cron logs).
	code, out, _ = run(t, "check", "--quiet", "-c", cfg)
	if code != ExitOK || out != "" {
		t.Fatalf("quiet: exit = %d, out = %q, want silence", code, out)
	}
}

func TestCheckDownServiceExitsOne(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	dir := t.TempDir()
	cfg := writeConfig(t, dir, httpService("Broken", srv.URL))

	code, out, errOut := run(t, "check", "-c", cfg)
	if code != ExitDown {
		t.Fatalf("exit = %d, want %d", code, ExitDown)
	}
	if !strings.Contains(out, "DOWN") || !strings.Contains(out, "status 500") {
		t.Errorf("out = %q", out)
	}
	if !strings.Contains(errOut, "1 of 1 service down") {
		t.Errorf("stderr = %q", errOut)
	}

	// A failed probe is still history — that is the whole point.
	recs, err := history.Load(filepath.Join(dir, "history.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].OK {
		t.Fatalf("history = %+v", recs)
	}
}

func TestCheckHistoryLifecycle(t *testing.T) {
	// Repeated checks accumulate records, and records older than
	// retention_days are pruned on the way.
	srv := healthyServer(t)
	dir := t.TempDir()
	cfg := writeConfig(t, dir, "retention_days = 7\n"+httpService("Website", srv.URL))

	// Seed a record far outside the retention window.
	old := history.Record{TS: fixedNow.AddDate(0, 0, -30), Service: "Website", OK: true}
	if err := history.Append(filepath.Join(dir, "history.jsonl"), []history.Record{old}); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		if code, _, e := run(t, "check", "--quiet", "-c", cfg); code != ExitOK {
			t.Fatalf("run %d: %s", i, e)
		}
	}
	recs, err := history.Load(filepath.Join(dir, "history.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 {
		t.Fatalf("history = %d records, want 3 fresh (expired one pruned)", len(recs))
	}
	for _, rec := range recs {
		if !rec.TS.Equal(fixedNow) {
			t.Fatalf("expired record survived: %+v", rec)
		}
	}
}

func TestCheckMissingConfigExits3(t *testing.T) {
	code, _, errOut := run(t, "check", "-c", filepath.Join(t.TempDir(), "absent.toml"))
	if code != ExitRuntime {
		t.Fatalf("exit = %d, want %d (stderr %q)", code, ExitRuntime, errOut)
	}
}

func TestCheckBuildProducesSite(t *testing.T) {
	srv := healthyServer(t)
	dir := t.TempDir()
	cfg := writeConfig(t, dir, httpService("Website", srv.URL))

	code, out, _ := run(t, "check", "--build", "-c", cfg)
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out, "index.html") {
		t.Errorf("out = %q", out)
	}
	page, err := os.ReadFile(filepath.Join(dir, "public", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), "All systems operational") {
		t.Error("page should show the operational banner")
	}
}

func TestBuildRendersHistoryAndIncidents(t *testing.T) {
	dir := t.TempDir()
	cfg := writeConfig(t, dir, httpService("Website", "http://127.0.0.1:1/"))

	// Build never probes: seed history by hand, including a failure.
	if err := history.Append(filepath.Join(dir, "history.jsonl"), []history.Record{
		{TS: fixedNow.Add(-2 * time.Hour), Service: "Website", OK: true, LatencyMS: 10},
		{TS: fixedNow.Add(-1 * time.Hour), Service: "Website", OK: false, Detail: "timeout"},
	}); err != nil {
		t.Fatal(err)
	}
	incDir := filepath.Join(dir, "incidents")
	if err := os.MkdirAll(incDir, 0o755); err != nil {
		t.Fatal(err)
	}
	incident := "---\ntitle: Website outage\ndate: 2026-07-12\nstatus: investigating\naffected: [Website]\n---\n\nLooking into it.\n"
	if err := os.WriteFile(filepath.Join(incDir, "2026-07-12-outage.md"), []byte(incident), 0o644); err != nil {
		t.Fatal(err)
	}

	code, _, errOut := run(t, "build", "-c", cfg)
	if code != ExitOK {
		t.Fatalf("exit = %d: %s", code, errOut)
	}

	page, err := os.ReadFile(filepath.Join(dir, "public", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Major outage", "timeout", "Website outage", "investigating"} {
		if !strings.Contains(string(page), want) {
			t.Errorf("page missing %q", want)
		}
	}

	var doc struct {
		Overall  string `json:"overall"`
		Services []struct {
			State string `json:"state"`
		} `json:"services"`
	}
	statusJSON, err := os.ReadFile(filepath.Join(dir, "public", "status.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(statusJSON, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Overall != "outage" || doc.Services[0].State != "down" {
		t.Errorf("status.json wrong: %+v", doc)
	}

	// Rebuilding two days "later" (different wall clock, same inputs) must
	// be byte-identical: the page is anchored to the inputs, not the clock.
	firstPage, firstJSON := page, statusJSON
	var out, errBuf2 bytes.Buffer
	later := &App{
		Stdout: &out,
		Stderr: &errBuf2,
		Prober: probe.New(),
		Now:    func() time.Time { return fixedNow.Add(48 * time.Hour) },
	}
	if code := later.Run(context.Background(), []string{"build", "-c", cfg}); code != ExitOK {
		t.Fatalf("rebuild exit = %d: %s", code, errBuf2.String())
	}
	page, err = os.ReadFile(filepath.Join(dir, "public", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	statusJSON, err = os.ReadFile(filepath.Join(dir, "public", "status.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(page, firstPage) {
		t.Error("index.html not byte-identical across rebuilds with a different clock")
	}
	if !bytes.Equal(statusJSON, firstJSON) {
		t.Error("status.json not byte-identical across rebuilds with a different clock")
	}
}

func TestBuildFailsOnBrokenIncident(t *testing.T) {
	dir := t.TempDir()
	cfg := writeConfig(t, dir, httpService("Website", "http://127.0.0.1:1/"))
	incDir := filepath.Join(dir, "incidents")
	if err := os.MkdirAll(incDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(incDir, "bad.md"), []byte("no front matter"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, _, errOut := run(t, "build", "-c", cfg)
	if code != ExitRuntime || !strings.Contains(errOut, "bad.md") {
		t.Fatalf("exit = %d, stderr = %q", code, errOut)
	}
}

func TestConfigRelativePathsResolveAgainstConfigDir(t *testing.T) {
	// Running `okpage check -c /elsewhere/okpage.toml` from any cwd must
	// use /elsewhere/history.jsonl, not ./history.jsonl.
	srv := healthyServer(t)
	dir := t.TempDir()
	cfg := writeConfig(t, dir, "history = \"data/probes.jsonl\"\noutput = \"www\"\n"+httpService("Website", srv.URL))

	if code, _, e := run(t, "check", "--build", "--quiet", "-c", cfg); code != ExitOK {
		t.Fatal(e)
	}
	if _, err := os.Stat(filepath.Join(dir, "data", "probes.jsonl")); err != nil {
		t.Error("history not resolved relative to the config file")
	}
	if _, err := os.Stat(filepath.Join(dir, "www", "index.html")); err != nil {
		t.Error("output not resolved relative to the config file")
	}
}

func TestFullPipelineInitCheckBuild(t *testing.T) {
	// The complete user journey with a mixed up/down fleet.
	up := healthyServer(t)
	downSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer downSrv.Close()

	dir := t.TempDir()
	cfg := writeConfig(t, dir,
		httpService("Good", up.URL),
		httpService("Bad", downSrv.URL),
	)

	code, out, _ := run(t, "check", "--build", "-c", cfg)
	if code != ExitDown {
		t.Fatalf("exit = %d, want %d", code, ExitDown)
	}
	if !strings.Contains(out, "1 up, 1 down") {
		t.Errorf("out = %q", out)
	}

	page, err := os.ReadFile(filepath.Join(dir, "public", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), "Partial outage") {
		t.Error("mixed fleet should render the partial-outage banner")
	}
	for _, badge := range []string{"good.svg", "bad.svg"} {
		if _, err := os.Stat(filepath.Join(dir, "public", "badge", badge)); err != nil {
			t.Errorf("badge %s missing", badge)
		}
	}
}

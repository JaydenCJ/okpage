// Tests for the JSONL history store: append/load round-trips, crash-safe
// pruning, and honest errors for corrupt files.
package history

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ts(day, hour int) time.Time {
	return time.Date(2026, 7, day, hour, 0, 0, 0, time.UTC)
}

func rec(day, hour int, service string, ok bool) Record {
	return Record{TS: ts(day, hour), Service: service, OK: ok, LatencyMS: 12, Detail: "200"}
}

func TestAppendThenLoadRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	in := []Record{rec(1, 10, "web", true), rec(1, 10, "db", false)}
	if err := Append(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("got %d records, want 2", len(out))
	}
	if !out[0].TS.Equal(in[0].TS) || out[0].Service != "web" || !out[0].OK {
		t.Errorf("first record mangled: %+v", out[0])
	}
	if out[1].Service != "db" || out[1].OK {
		t.Errorf("second record mangled: %+v", out[1])
	}
}

func TestAppendCreatesParentDirsAndAccumulates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "nested", "history.jsonl")
	for i := 0; i < 3; i++ {
		if err := Append(path, []Record{rec(1, 10+i, "web", true)}); err != nil {
			t.Fatal(err)
		}
	}
	out, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("got %d records, want 3 — append must not truncate", len(out))
	}
}

func TestAppendNothingIsANoOp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	if err := Append(path, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("empty append should not create the file")
	}
}

func TestLoadMissingFileIsEmptyHistory(t *testing.T) {
	out, err := Load(filepath.Join(t.TempDir(), "absent.jsonl"))
	if err != nil {
		t.Fatalf("missing history must not be an error: %v", err)
	}
	if out != nil {
		t.Fatalf("got %v, want nil", out)
	}
}

func TestLoadSkipsBlankLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	content := `{"ts":"2026-07-01T10:00:00Z","service":"web","ok":true,"latency_ms":5}` + "\n\n" +
		`{"ts":"2026-07-01T11:00:00Z","service":"web","ok":false,"latency_ms":0,"detail":"timeout"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("got %d records, want 2", len(out))
	}
	if out[1].Detail != "timeout" {
		t.Errorf("detail lost: %+v", out[1])
	}
}

func TestLoadReportsCorruptLineWithNumber(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	content := `{"ts":"2026-07-01T10:00:00Z","service":"web","ok":true,"latency_ms":5}` + "\n" +
		`{"ts": TRUNCATED` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("want an error naming line 2, got %v", err)
	}
}

func TestPruneDropsOnlyExpiredRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	if err := Append(path, []Record{
		rec(1, 10, "web", true),
		rec(5, 10, "web", true),
		rec(9, 10, "web", true),
	}); err != nil {
		t.Fatal(err)
	}
	dropped, err := Prune(path, ts(5, 0))
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 1 {
		t.Fatalf("dropped = %d, want 1", dropped)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || !out[0].TS.Equal(ts(5, 10)) {
		t.Fatalf("wrong records survived: %+v", out)
	}
}

func TestPruneKeepsRecordExactlyAtCutoff(t *testing.T) {
	// Boundary semantics matter: a record stamped exactly at the cutoff is
	// still inside the retention window.
	path := filepath.Join(t.TempDir(), "history.jsonl")
	if err := Append(path, []Record{rec(5, 0, "web", true)}); err != nil {
		t.Fatal(err)
	}
	dropped, err := Prune(path, ts(5, 0))
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 0 {
		t.Fatalf("record at cutoff must be kept, dropped %d", dropped)
	}
}

func TestPruneWithNothingToDropLeavesFileUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	if err := Append(path, []Record{rec(8, 10, "web", true)}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Prune(path, ts(1, 0)); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("no-op prune must not rewrite the file")
	}

	// Pruning a history that does not exist yet must also succeed quietly.
	if _, err := Prune(filepath.Join(t.TempDir(), "absent.jsonl"), ts(1, 0)); err != nil {
		t.Fatalf("pruning a missing history must not fail: %v", err)
	}
}

func TestPruneLeavesNoTempFilesBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.jsonl")
	if err := Append(path, []Record{rec(1, 10, "web", true), rec(9, 10, "web", true)}); err != nil {
		t.Fatal(err)
	}
	if _, err := Prune(path, ts(5, 0)); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "history.jsonl" {
		t.Fatalf("stray files after prune: %v", entries)
	}
}

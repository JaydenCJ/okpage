// Package history persists probe results as an append-only JSON-lines file.
//
// JSONL was chosen over a database on purpose: `okpage check` from cron only
// ever appends one line per service, the file diffs and syncs cleanly
// (git, rsync, object storage), and a partial last line — the worst a crash
// can leave behind — is detected and reported instead of corrupting reads.
package history

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Record is one probe outcome at one point in time.
type Record struct {
	TS        time.Time `json:"ts"`
	Service   string    `json:"service"`
	OK        bool      `json:"ok"`
	LatencyMS int64     `json:"latency_ms"`
	Detail    string    `json:"detail,omitempty"`
}

// Append writes records to the end of the file at path, creating parent
// directories and the file itself if needed. One JSON object per line.
func Append(path string, records []Record) error {
	if len(records) == 0 {
		return nil
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	for _, rec := range records {
		if err := enc.Encode(rec); err != nil {
			return err
		}
	}
	return w.Flush()
}

// Load reads every record from path. A missing file is an empty history,
// not an error — the first `okpage check` starts from nothing.
func Load(path string) ([]Record, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var records []Record
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec Record
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil, fmt.Errorf("%s: line %d: %v", path, lineNo, err)
		}
		records = append(records, rec)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	return records, nil
}

// Prune rewrites the file keeping only records at or after cutoff. The
// rewrite is atomic (temp file + rename) so a crash mid-prune can never
// leave a half-written history. Returns how many records were dropped.
func Prune(path string, cutoff time.Time) (int, error) {
	records, err := Load(path)
	if err != nil {
		return 0, err
	}
	kept := records[:0]
	for _, rec := range records {
		if !rec.TS.Before(cutoff) {
			kept = append(kept, rec)
		}
	}
	dropped := len(records) - len(kept)
	if dropped == 0 {
		return 0, nil
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".okpage-history-*")
	if err != nil {
		return 0, err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename

	w := bufio.NewWriter(tmp)
	enc := json.NewEncoder(w)
	for _, rec := range kept {
		if err := enc.Encode(rec); err != nil {
			tmp.Close()
			return 0, err
		}
	}
	if err := w.Flush(); err != nil {
		tmp.Close()
		return 0, err
	}
	if err := tmp.Close(); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return 0, err
	}
	return dropped, nil
}

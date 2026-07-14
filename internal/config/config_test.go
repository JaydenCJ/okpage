// Tests for config validation: defaults, per-probe-type requirements, and
// the typo-catching unknown-key rejection.
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const minimal = `
[[service]]
name = "Website"
type = "http"
url = "http://127.0.0.1:8080/health"
`

func TestParseAppliesDefaults(t *testing.T) {
	cfg, err := Parse(minimal)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Title != "Status" {
		t.Errorf("Title = %q, want default", cfg.Title)
	}
	if cfg.Output != "public" || cfg.History != "history.jsonl" || cfg.Incidents != "incidents" {
		t.Errorf("path defaults wrong: %+v", cfg)
	}
	if cfg.RetentionDays != 90 || cfg.Days != 90 {
		t.Errorf("day defaults wrong: %+v", cfg)
	}
	if cfg.Timeout != 10*time.Second {
		t.Errorf("Timeout = %v, want 10s", cfg.Timeout)
	}
}

func TestParseReadsAllTopLevelKeys(t *testing.T) {
	cfg, err := Parse(`
title = "Acme Status"
output = "dist"
history = "data/history.jsonl"
incidents = "outages"
retention_days = 30
days = 45
timeout = "5s"
` + minimal)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Title != "Acme Status" || cfg.Output != "dist" ||
		cfg.History != "data/history.jsonl" || cfg.Incidents != "outages" {
		t.Errorf("strings not read: %+v", cfg)
	}
	if cfg.RetentionDays != 30 || cfg.Days != 45 || cfg.Timeout != 5*time.Second {
		t.Errorf("numbers not read: %+v", cfg)
	}
}

func TestParseServiceFieldsAndPerServiceTimeout(t *testing.T) {
	cfg, err := Parse(`
[[service]]
name = "API"
type = "http"
url = "https://api.example.test/ping"
method = "head"
expect_status = 204
expect_body = "pong"
timeout = "2s"
`)
	if err != nil {
		t.Fatal(err)
	}
	svc := cfg.Services[0]
	if svc.Method != "HEAD" {
		t.Errorf("method not upper-cased: %q", svc.Method)
	}
	if svc.ExpectStatus != 204 || svc.ExpectBody != "pong" {
		t.Errorf("expectations not read: %+v", svc)
	}
	if svc.Timeout != 2*time.Second {
		t.Errorf("per-service timeout not read: %v", svc.Timeout)
	}
}

func TestParseServiceDefaults(t *testing.T) {
	cfg, err := Parse(`
[[service]]
name = "Website"
url = "http://127.0.0.1/"
`)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Services[0].Type != "http" {
		t.Errorf("Type = %q, want http (the default probe type)", cfg.Services[0].Type)
	}
	if cfg.Services[0].Method != "GET" {
		t.Errorf("Method = %q, want GET", cfg.Services[0].Method)
	}
}

// TestParseRejectsInvalidConfigs covers every validation rule. The messages
// are asserted too — a config error at 3 a.m. must say what to fix.
func TestParseRejectsInvalidConfigs(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"no services", `title = "x"`, "nothing to probe"},
		{"typo in top-level key", "retenton_days = 30\n" + minimal, `unknown key "retenton_days"`},
		{"typo in service key", minimal + "expect_staus = 200\n", `unknown key "expect_staus"`},
		{"duplicate service names", minimal + minimal, "duplicate service name"},
		{"unnamed service", "[[service]]\ntype = \"dns\"\nhostname = \"example.test\"\nname = \"\"", `"name" is required`},
		{"unknown probe type", "[[service]]\nname = \"x\"\ntype = \"icmp\"", `unknown probe type "icmp"`},
		{"http without url", "[[service]]\nname = \"x\"\ntype = \"http\"", `require "url"`},
		{"non-http scheme", "[[service]]\nname = \"x\"\ntype = \"http\"\nurl = \"ftp://example.test/\"", "http:// or https://"},
		{"bad method", minimal + "method = \"POST\"", "GET or HEAD"},
		{"out-of-range expect_status", minimal + "expect_status = 42", "valid HTTP status"},
		{"tcp without port", "[[service]]\nname = \"db\"\ntype = \"tcp\"\naddress = \"127.0.0.1\"", "host:port"},
		{"dns without hostname", "[[service]]\nname = \"d\"\ntype = \"dns\"", `require "hostname"`},
		{"unparseable duration", "timeout = \"soon\"\n" + minimal, "positive duration"},
		{"negative duration", "timeout = \"-3s\"\n" + minimal, "positive duration"},
		{"wrong value type", "title = 42\n" + minimal, `"title" must be a string`},
		{"service as scalar", `service = "oops"`, "[[service]]"},
		{"days too small", "days = 0\n" + minimal, "between 1 and 365"},
		{"days too large", "days = 999\n" + minimal, "between 1 and 365"},
		{"retention too small", "retention_days = 0\n" + minimal, "at least 1"},
	}
	for _, tc := range cases {
		_, err := Parse(tc.src)
		if err == nil {
			t.Errorf("%s: expected error containing %q, got nil", tc.name, tc.want)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %q does not contain %q", tc.name, err, tc.want)
		}
	}
}

func TestLoadReadsFileAndPrefixesErrorsWithPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "okpage.toml")
	if err := os.WriteFile(path, []byte(minimal), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Services) != 1 {
		t.Fatalf("services = %d, want 1", len(cfg.Services))
	}

	bad := filepath.Join(dir, "bad.toml")
	if err := os.WriteFile(bad, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(bad); err == nil || !strings.Contains(err.Error(), "bad.toml") {
		t.Fatalf("error should name the file, got %v", err)
	}
}

func TestLoadMissingFileReturnsError(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "absent.toml")); !os.IsNotExist(err) {
		t.Fatalf("want IsNotExist, got %v", err)
	}
}

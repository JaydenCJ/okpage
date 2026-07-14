// Package config loads and validates okpage.toml, the single file that
// declares which services to probe and where the static site goes.
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

// Defaults applied when a key is absent from the config file.
const (
	DefaultOutput        = "public"
	DefaultHistory       = "history.jsonl"
	DefaultIncidents     = "incidents"
	DefaultRetentionDays = 90
	DefaultTimeout       = 10 * time.Second
	DefaultDays          = 90
)

// Config is the fully validated okpage configuration.
type Config struct {
	Title         string        // page heading, e.g. "Acme Status"
	Output        string        // directory the static site is written to
	History       string        // JSON-lines probe history file
	Incidents     string        // directory holding incident markdown files
	RetentionDays int           // history records older than this are pruned
	Days          int           // number of daily bars rendered per service
	Timeout       time.Duration // default per-probe timeout
	Services      []Service
}

// Service is one probed target.
type Service struct {
	Name         string
	Type         string        // "http", "tcp", or "dns"
	URL          string        // http: full URL
	Method       string        // http: GET (default) or HEAD
	ExpectStatus int           // http: exact status; 0 means any 2xx
	ExpectBody   string        // http: required response-body substring
	Address      string        // tcp: host:port
	Hostname     string        // dns: name that must resolve
	Timeout      time.Duration // per-service override; 0 means Config.Timeout
}

// Load reads and validates the config file at path. Paths inside the config
// (output, history, incidents) are resolved by the caller relative to the
// config file's directory, not here.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg, err := Parse(string(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Parse validates config file contents.
func Parse(src string) (*Config, error) {
	doc, err := parseTOML(src)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		Title:         "Status",
		Output:        DefaultOutput,
		History:       DefaultHistory,
		Incidents:     DefaultIncidents,
		RetentionDays: DefaultRetentionDays,
		Days:          DefaultDays,
		Timeout:       DefaultTimeout,
	}

	top := scope{table: map[string]any(doc), where: "top level"}
	cfg.Title = top.str("title", cfg.Title)
	cfg.Output = top.str("output", cfg.Output)
	cfg.History = top.str("history", cfg.History)
	cfg.Incidents = top.str("incidents", cfg.Incidents)
	cfg.RetentionDays = top.intv("retention_days", cfg.RetentionDays)
	cfg.Days = top.intv("days", cfg.Days)
	cfg.Timeout = top.duration("timeout", cfg.Timeout)

	rawServices, _ := doc["service"].([]map[string]any)
	if doc["service"] != nil && rawServices == nil {
		return nil, fmt.Errorf("\"service\" must be declared as [[service]] blocks")
	}
	top.claim("service")

	for i, raw := range rawServices {
		s := scope{table: raw, where: fmt.Sprintf("service #%d", i+1)}
		svc := Service{
			Name:         s.str("name", ""),
			Type:         s.str("type", "http"),
			URL:          s.str("url", ""),
			Method:       strings.ToUpper(s.str("method", "GET")),
			ExpectStatus: s.intv("expect_status", 0),
			ExpectBody:   s.str("expect_body", ""),
			Address:      s.str("address", ""),
			Hostname:     s.str("hostname", ""),
			Timeout:      s.duration("timeout", 0),
		}
		if s.err != nil {
			return nil, s.err
		}
		if err := s.unknown(); err != nil {
			return nil, err
		}
		if err := validateService(&svc, i); err != nil {
			return nil, err
		}
		cfg.Services = append(cfg.Services, svc)
	}

	if top.err != nil {
		return nil, top.err
	}
	if err := top.unknown(); err != nil {
		return nil, err
	}
	if err := validate(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func validate(cfg *Config) error {
	if len(cfg.Services) == 0 {
		return fmt.Errorf("no [[service]] blocks defined — nothing to probe")
	}
	if cfg.RetentionDays < 1 {
		return fmt.Errorf("retention_days must be at least 1")
	}
	if cfg.Days < 1 || cfg.Days > 365 {
		return fmt.Errorf("days must be between 1 and 365")
	}
	if cfg.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	seen := map[string]bool{}
	for _, svc := range cfg.Services {
		if seen[svc.Name] {
			return fmt.Errorf("duplicate service name %q — names must be unique", svc.Name)
		}
		seen[svc.Name] = true
	}
	return nil
}

func validateService(svc *Service, i int) error {
	where := fmt.Sprintf("service #%d", i+1)
	if svc.Name == "" {
		return fmt.Errorf("%s: \"name\" is required", where)
	}
	where = fmt.Sprintf("service %q", svc.Name)

	switch svc.Type {
	case "http":
		if svc.URL == "" {
			return fmt.Errorf("%s: http probes require \"url\"", where)
		}
		u, err := url.Parse(svc.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("%s: \"url\" must be an absolute http:// or https:// URL", where)
		}
		if svc.Method != "GET" && svc.Method != "HEAD" {
			return fmt.Errorf("%s: \"method\" must be GET or HEAD", where)
		}
		if svc.ExpectStatus != 0 && (svc.ExpectStatus < 100 || svc.ExpectStatus > 599) {
			return fmt.Errorf("%s: \"expect_status\" must be a valid HTTP status code", where)
		}
	case "tcp":
		if svc.Address == "" {
			return fmt.Errorf("%s: tcp probes require \"address\"", where)
		}
		if _, _, err := net.SplitHostPort(svc.Address); err != nil {
			return fmt.Errorf("%s: \"address\" must be host:port", where)
		}
	case "dns":
		if svc.Hostname == "" {
			return fmt.Errorf("%s: dns probes require \"hostname\"", where)
		}
	default:
		return fmt.Errorf("%s: unknown probe type %q (want http, tcp, or dns)", where, svc.Type)
	}
	if svc.Timeout < 0 {
		return fmt.Errorf("%s: \"timeout\" must be positive", where)
	}
	return nil
}

// scope reads typed values out of one parsed table, remembers which keys were
// consumed, and records the first type error it encounters.
type scope struct {
	table   map[string]any
	where   string
	err     error
	claimed map[string]bool
}

func (s *scope) claim(key string) {
	if s.claimed == nil {
		s.claimed = map[string]bool{}
	}
	s.claimed[key] = true
}

func (s *scope) fail(key, want string, got any) {
	if s.err == nil {
		s.err = fmt.Errorf("%s: %q must be %s, got %v", s.where, key, want, got)
	}
}

func (s *scope) str(key, def string) string {
	s.claim(key)
	v, ok := s.table[key]
	if !ok {
		return def
	}
	str, ok := v.(string)
	if !ok {
		s.fail(key, "a string", v)
		return def
	}
	return str
}

func (s *scope) intv(key string, def int) int {
	s.claim(key)
	v, ok := s.table[key]
	if !ok {
		return def
	}
	n, ok := v.(int64)
	if !ok {
		s.fail(key, "an integer", v)
		return def
	}
	return int(n)
}

func (s *scope) duration(key string, def time.Duration) time.Duration {
	s.claim(key)
	v, ok := s.table[key]
	if !ok {
		return def
	}
	str, ok := v.(string)
	if !ok {
		s.fail(key, `a duration string like "10s"`, v)
		return def
	}
	d, err := time.ParseDuration(str)
	if err != nil || d <= 0 {
		s.fail(key, `a positive duration like "10s" or "1m"`, str)
		return def
	}
	return d
}

// unknown rejects keys that were never claimed, catching config typos such
// as `expect_staus` before they silently disable a check.
func (s *scope) unknown() error {
	for key := range s.table {
		if !s.claimed[key] {
			return fmt.Errorf("%s: unknown key %q", s.where, key)
		}
	}
	return nil
}

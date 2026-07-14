// Tests for the probe engine. HTTP and TCP probes run against loopback
// listeners started inside the test; DNS uses an injected fake resolver.
// Nothing here touches a real network or sleeps.
package probe

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JaydenCJ/okpage/internal/config"
)

func testProber() *Prober {
	p := New()
	p.Resolve = func(ctx context.Context, host string) ([]string, error) {
		return nil, fmt.Errorf("no DNS in tests: %s", host)
	}
	return p
}

func runOne(t *testing.T, p *Prober, svc config.Service) Result {
	t.Helper()
	results := p.Run(context.Background(), []config.Service{svc}, 5*time.Second)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	return results[0]
}

func TestHTTPProbeDefaultStatusPolicyIs2xx(t *testing.T) {
	status := http.StatusNoContent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
	}))
	defer srv.Close()
	svc := config.Service{Name: "web", Type: "http", URL: srv.URL, Method: "GET"}

	res := runOne(t, testProber(), svc)
	if !res.OK {
		t.Fatalf("204 should pass by default, got detail %q", res.Detail)
	}
	if res.Detail != "204" {
		t.Errorf("detail = %q, want the status code", res.Detail)
	}

	status = http.StatusServiceUnavailable
	res = runOne(t, testProber(), svc)
	if res.OK {
		t.Fatal("503 must fail")
	}
	if res.Detail != "status 503 (want 2xx)" {
		t.Errorf("detail = %q", res.Detail)
	}
}

func TestHTTPProbeExactExpectStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer srv.Close()

	svc := config.Service{Name: "web", Type: "http", URL: srv.URL, Method: "GET", ExpectStatus: 418}
	if res := runOne(t, testProber(), svc); !res.OK {
		t.Fatalf("exact 418 expectation should pass, got %q", res.Detail)
	}

	svc.ExpectStatus = 200
	res := runOne(t, testProber(), svc)
	if res.OK {
		t.Fatal("418 vs expected 200 must fail")
	}
	if res.Detail != "status 418 (want 200)" {
		t.Errorf("detail = %q", res.Detail)
	}
}

func TestHTTPProbeExpectBodySubstring(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"healthy","version":"3"}`)
	}))
	defer srv.Close()

	svc := config.Service{Name: "api", Type: "http", URL: srv.URL, Method: "GET", ExpectBody: `"healthy"`}
	if res := runOne(t, testProber(), svc); !res.OK {
		t.Fatalf("substring present, got %q", res.Detail)
	}

	svc.ExpectBody = "degraded"
	res := runOne(t, testProber(), svc)
	if res.OK {
		t.Fatal("missing substring must fail")
	}
	if !strings.Contains(res.Detail, `does not contain "degraded"`) {
		t.Errorf("detail = %q", res.Detail)
	}
}

func TestHTTPProbeSendsMethodAndUserAgent(t *testing.T) {
	var gotMethod, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotUA = r.Method, r.UserAgent()
	}))
	defer srv.Close()

	runOne(t, testProber(), config.Service{Name: "web", Type: "http", URL: srv.URL, Method: "HEAD"})
	if gotMethod != "HEAD" {
		t.Errorf("method = %q, want HEAD", gotMethod)
	}
	if !strings.HasPrefix(gotUA, "okpage/") {
		t.Errorf("User-Agent = %q, want okpage/<version>", gotUA)
	}
}

func TestHTTPProbeConnectionRefused(t *testing.T) {
	// Grab a loopback port and close it so nothing is listening.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	url := "http://" + ln.Addr().String() + "/"
	ln.Close()

	res := runOne(t, testProber(), config.Service{Name: "web", Type: "http", URL: url, Method: "GET"})
	if res.OK {
		t.Fatal("refused connection must fail")
	}
	if !strings.Contains(res.Detail, "refused") {
		t.Errorf("detail = %q, want a refused-connection message", res.Detail)
	}
	if strings.Contains(res.Detail, "://") {
		t.Errorf("detail %q should not repeat the URL", res.Detail)
	}
}

func TestHTTPProbeCanceledContext(t *testing.T) {
	// A pre-canceled context must fail fast with a clean detail, never hang.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p := testProber()
	results := p.Run(ctx, []config.Service{
		{Name: "web", Type: "http", URL: "http://127.0.0.1:1/", Method: "GET"},
	}, 5*time.Second)
	if results[0].OK {
		t.Fatal("canceled probe must fail")
	}
}

func TestTCPProbeConnectsAndReportsRefusal(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()
	addr := ln.Addr().String()
	svc := config.Service{Name: "db", Type: "tcp", Address: addr}

	res := runOne(t, testProber(), svc)
	if !res.OK {
		t.Fatalf("listener up, got %q", res.Detail)
	}
	if res.Detail != "connected" {
		t.Errorf("detail = %q", res.Detail)
	}

	// Close the listener; the same port must now report a refusal.
	ln.Close()
	res = runOne(t, testProber(), svc)
	if res.OK {
		t.Fatal("closed port must fail")
	}
	if !strings.Contains(res.Detail, "refused") {
		t.Errorf("detail = %q", res.Detail)
	}
}

func TestDNSProbeOutcomes(t *testing.T) {
	cases := []struct {
		name       string
		resolve    func(ctx context.Context, host string) ([]string, error)
		wantOK     bool
		wantDetail string
	}{
		{
			"resolves",
			func(ctx context.Context, host string) ([]string, error) {
				return []string{"192.0.2.1", "192.0.2.2"}, nil
			},
			true, "2 addresses",
		},
		{
			// Exactly one answer must read "1 address", not "1 addresses".
			"resolves single",
			func(ctx context.Context, host string) ([]string, error) {
				return []string{"192.0.2.1"}, nil
			},
			true, "1 address",
		},
		{
			// An empty-but-successful answer means the name effectively does
			// not exist; it must count as down, not up.
			"empty answer",
			func(ctx context.Context, host string) ([]string, error) { return nil, nil },
			false, "resolved 0 addresses",
		},
		{
			"resolver error",
			func(ctx context.Context, host string) ([]string, error) {
				return nil, fmt.Errorf("NXDOMAIN")
			},
			false, "NXDOMAIN",
		},
	}
	for _, tc := range cases {
		p := testProber()
		p.Resolve = tc.resolve
		res := runOne(t, p, config.Service{Name: "dns", Type: "dns", Hostname: "example.test"})
		if res.OK != tc.wantOK {
			t.Errorf("%s: OK = %v, want %v", tc.name, res.OK, tc.wantOK)
		}
		if !strings.Contains(res.Detail, tc.wantDetail) {
			t.Errorf("%s: detail = %q, want %q", tc.name, res.Detail, tc.wantDetail)
		}
	}
}

func TestRunPreservesConfigOrderUnderConcurrency(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	var services []config.Service
	for i := 0; i < 12; i++ {
		services = append(services, config.Service{
			Name: fmt.Sprintf("svc-%02d", i), Type: "http", URL: srv.URL, Method: "GET",
		})
	}
	results := testProber().Run(context.Background(), services, 5*time.Second)
	for i, res := range results {
		if want := fmt.Sprintf("svc-%02d", i); res.Service != want {
			t.Fatalf("results[%d] = %q, want %q — order must match the config", i, res.Service, want)
		}
	}
}

func TestRunMixesProbeTypes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	p := testProber()
	p.Resolve = func(ctx context.Context, host string) ([]string, error) {
		return []string{"192.0.2.1"}, nil
	}
	results := p.Run(context.Background(), []config.Service{
		{Name: "web", Type: "http", URL: srv.URL, Method: "GET"},
		{Name: "db", Type: "tcp", Address: ln.Addr().String()},
		{Name: "dns", Type: "dns", Hostname: "example.test"},
	}, 5*time.Second)
	for _, res := range results {
		if !res.OK {
			t.Errorf("%s failed: %q", res.Service, res.Detail)
		}
	}
}

func TestLatencyIsMeasuredWithInjectedClock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	// A fake clock that advances 250 ms per reading makes the latency math
	// exact without any real waiting.
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	calls := 0
	p := testProber()
	p.Now = func() time.Time {
		calls++
		return base.Add(time.Duration(calls-1) * 250 * time.Millisecond)
	}
	res := runOne(t, p, config.Service{Name: "web", Type: "http", URL: srv.URL, Method: "GET"})
	if res.Latency != 250*time.Millisecond {
		t.Fatalf("latency = %v, want exactly 250ms from the fake clock", res.Latency)
	}
}

func TestDescribeErrProducesCleanDetails(t *testing.T) {
	// url.Error prefixes repeat the configured URL; strip them but keep
	// the underlying cause.
	err := fmt.Errorf(`Get "http://127.0.0.1:9/x": dial tcp 127.0.0.1:9: connect: connection refused`)
	got := describeErr(err)
	if strings.Contains(got, "Get \"") {
		t.Errorf("prefix not stripped: %q", got)
	}
	if !strings.Contains(got, "connection refused") {
		t.Errorf("cause lost: %q", got)
	}

	// Deadline errors are named "timeout", even when wrapped.
	if got := describeErr(context.DeadlineExceeded); got != "timeout" {
		t.Errorf("got %q, want timeout", got)
	}
	if got := describeErr(fmt.Errorf("wrapped: %w", context.DeadlineExceeded)); got != "timeout" {
		t.Errorf("wrapped: got %q, want timeout", got)
	}
}

// Package probe executes health checks against configured services.
//
// Three probe types are supported: http (request a URL, assert on status and
// body), tcp (open a connection), and dns (resolve a hostname). Probes run
// concurrently with independent timeouts; results are returned in config
// order so all downstream output is deterministic.
package probe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/JaydenCJ/okpage/internal/config"
	"github.com/JaydenCJ/okpage/internal/version"
)

// maxBody caps how much of an HTTP response is read when checking
// expect_body, so a misconfigured endpoint cannot make okpage buffer
// gigabytes.
const maxBody = 1 << 20 // 1 MiB

// Result is the outcome of one probe.
type Result struct {
	Service string        // service name from the config
	OK      bool          // did the probe pass
	Latency time.Duration // wall time the probe took
	Detail  string        // failure reason, or e.g. "200" / "connected" on success
}

// Doer is the slice of *http.Client the prober needs; tests substitute fakes.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Prober runs probes. The zero value is not usable; call New.
type Prober struct {
	Client  Doer
	Dial    func(ctx context.Context, network, address string) (net.Conn, error)
	Resolve func(ctx context.Context, host string) ([]string, error)
	Now     func() time.Time
}

// New returns a Prober wired to the real network. Redirects are followed
// (up to the net/http default of 10); TLS uses system roots.
func New() *Prober {
	dialer := &net.Dialer{}
	resolver := &net.Resolver{}
	return &Prober{
		Client:  &http.Client{},
		Dial:    dialer.DialContext,
		Resolve: resolver.LookupHost,
		Now:     time.Now,
	}
}

// Run probes every service concurrently and returns one Result per service,
// in the same order as the input.
func (p *Prober) Run(ctx context.Context, services []config.Service, defaultTimeout time.Duration) []Result {
	results := make([]Result, len(services))
	var wg sync.WaitGroup
	for i, svc := range services {
		wg.Add(1)
		go func(i int, svc config.Service) {
			defer wg.Done()
			results[i] = p.one(ctx, svc, defaultTimeout)
		}(i, svc)
	}
	wg.Wait()
	return results
}

// one runs a single probe with its effective timeout applied.
func (p *Prober) one(ctx context.Context, svc config.Service, defaultTimeout time.Duration) Result {
	timeout := svc.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := p.Now()
	ok, detail := p.dispatch(ctx, svc)
	return Result{
		Service: svc.Name,
		OK:      ok,
		Latency: p.Now().Sub(start),
		Detail:  detail,
	}
}

func (p *Prober) dispatch(ctx context.Context, svc config.Service) (bool, string) {
	switch svc.Type {
	case "http":
		return p.httpProbe(ctx, svc)
	case "tcp":
		return p.tcpProbe(ctx, svc)
	case "dns":
		return p.dnsProbe(ctx, svc)
	default:
		// Unreachable for validated configs; report rather than panic.
		return false, fmt.Sprintf("unknown probe type %q", svc.Type)
	}
}

func (p *Prober) httpProbe(ctx context.Context, svc config.Service) (bool, string) {
	req, err := http.NewRequestWithContext(ctx, svc.Method, svc.URL, nil)
	if err != nil {
		return false, describeErr(err)
	}
	req.Header.Set("User-Agent", "okpage/"+version.Version)

	resp, err := p.Client.Do(req)
	if err != nil {
		return false, describeErr(err)
	}
	defer resp.Body.Close()

	if svc.ExpectStatus != 0 {
		if resp.StatusCode != svc.ExpectStatus {
			return false, fmt.Sprintf("status %d (want %d)", resp.StatusCode, svc.ExpectStatus)
		}
	} else if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return false, fmt.Sprintf("status %d (want 2xx)", resp.StatusCode)
	}

	if svc.ExpectBody != "" {
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
		if err != nil {
			return false, describeErr(err)
		}
		if !strings.Contains(string(body), svc.ExpectBody) {
			return false, fmt.Sprintf("body does not contain %q", svc.ExpectBody)
		}
	}
	return true, fmt.Sprintf("%d", resp.StatusCode)
}

func (p *Prober) tcpProbe(ctx context.Context, svc config.Service) (bool, string) {
	conn, err := p.Dial(ctx, "tcp", svc.Address)
	if err != nil {
		return false, describeErr(err)
	}
	conn.Close()
	return true, "connected"
}

func (p *Prober) dnsProbe(ctx context.Context, svc config.Service) (bool, string) {
	addrs, err := p.Resolve(ctx, svc.Hostname)
	if err != nil {
		return false, describeErr(err)
	}
	if len(addrs) == 0 {
		return false, "resolved 0 addresses"
	}
	if len(addrs) == 1 {
		return true, "1 address"
	}
	return true, fmt.Sprintf("%d addresses", len(addrs))
}

// describeErr flattens transport errors into a short, single-line detail.
// Timeouts and cancellations are named explicitly because raw context errors
// ("context deadline exceeded" wrapped in url.Error noise) read poorly on a
// status page.
func describeErr(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	msg := err.Error()
	// url.Error prefixes like `Get "http://…": ` repeat the config; strip them.
	if i := strings.Index(msg, `": `); i >= 0 && strings.Contains(msg[:i], "://") {
		msg = msg[i+3:]
	}
	return strings.ReplaceAll(msg, "\n", " ")
}

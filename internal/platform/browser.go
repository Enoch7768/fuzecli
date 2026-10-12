package platform

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type BrowserSession struct {
	ID       string        `json:"id"`
	URL      string        `json:"url"`
	Status   int           `json:"status"`
	Duration time.Duration `json:"duration"`
}

func InspectURL(ctx context.Context, rawURL string) (BrowserSession, error) {
	if ctx == nil {
		return BrowserSession{}, errors.New("browser inspection context is required")
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return BrowserSession{}, err
	}
	if u.User != nil {
		return BrowserSession{}, errors.New("browser inspection does not permit URL credentials")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return BrowserSession{}, errors.New("browser inspection requires http or https")
	}
	host := u.Hostname()
	if host == "" {
		return BrowserSession{}, errors.New("browser inspection requires a host")
	}
	if ip := net.ParseIP(host); ip != nil {
		if !ip.IsLoopback() {
			return BrowserSession{}, errors.New("browser inspection only permits loopback IPs")
		}
	} else if !strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") {
		return BrowserSession{}, errors.New("browser inspection only permits localhost or loopback hosts")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return BrowserSession{}, err
	}
	start := time.Now()
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return dialLoopback(ctx, network, address)
		},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	defer transport.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return BrowserSession{}, err
	}
	defer resp.Body.Close()
	return BrowserSession{
		ID:       time.Now().UTC().Format("20060102T150405.000000000Z"),
		URL:      u.String(),
		Status:   resp.StatusCode,
		Duration: time.Since(start),
	}, nil
}

func dialLoopback(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ips := []net.IP{}
	if ip := net.ParseIP(host); ip != nil {
		ips = append(ips, ip)
	} else {
		lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		resolved, err := net.DefaultResolver.LookupIPAddr(lookupCtx, host)
		if err != nil {
			return nil, err
		}
		for _, result := range resolved {
			ips = append(ips, result.IP)
		}
	}
	if len(ips) == 0 {
		return nil, errors.New("localhost did not resolve to an IP address")
	}
	for _, ip := range ips {
		if !ip.IsLoopback() {
			return nil, fmt.Errorf("browser inspection blocked non-loopback resolution for %q", host)
		}
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
}

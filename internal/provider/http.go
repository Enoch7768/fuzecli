package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

func DoJSON(ctx context.Context, client *http.Client, method, url string, headers map[string]string, body any, out any, providerName string) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode %s request: %w", providerName, err)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create %s request: %w", providerName, err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return &ProviderError{Kind: ErrorProviderUnavailable, Provider: providerName, Message: "request failed", Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parseProviderHTTPError(resp, providerName)
	}
	if out == nil {
		io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s response: %w", providerName, err)
	}
	return nil
}

func parseProviderHTTPError(resp *http.Response, providerName string) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
	msg := strings.TrimSpace(string(b))
	kind := ErrorUnknown
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		kind = ErrorUnauthorized
	case http.StatusTooManyRequests:
		kind = ErrorRateLimited
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		kind = ErrorProviderUnavailable
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		kind = ErrorBadRequest
	}
	retryAfter := 0
	if s := resp.Header.Get("Retry-After"); s != "" {
		retryAfter, _ = strconv.Atoi(s)
	}
	return &ProviderError{Kind: kind, Provider: providerName, StatusCode: resp.StatusCode, Message: msg, RetryAfter: retryAfter}
}

func ReadSSE(ctx context.Context, resp *http.Response, onData func(string) error) error {
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			if err := onData(strings.TrimSpace(strings.TrimPrefix(line, "data:"))); err != nil {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

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

func DoJSON(
	ctx context.Context,
	client *http.Client,
	method string,
	url string,
	headers map[string]string,
	body any,
	out any,
	providerName string,
) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf(
			"encode %s request: %w",
			providerName,
			err,
		)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		method,
		url,
		bytes.NewReader(data),
	)
	if err != nil {
		return fmt.Errorf(
			"create %s request: %w",
			providerName,
			err,
		)
	}

	request.Header.Set(
		"Content-Type",
		"application/json",
	)

	for key, value := range headers {
		request.Header.Set(key, value)
	}

	response, err := client.Do(request)
	if err != nil {
		return &ProviderError{
			Kind:     ErrorProviderUnavailable,
			Provider: providerName,
			Message:  "request failed",
			Err:      err,
		}
	}

	defer response.Body.Close()

	if response.StatusCode < 200 ||
		response.StatusCode >= 300 {
		return ParseHTTPResponseError(
			response,
			providerName,
		)
	}

	if out == nil {
		_, _ = io.Copy(
			io.Discard,
			response.Body,
		)
		return nil
	}

	if err := json.NewDecoder(
		response.Body,
	).Decode(out); err != nil {
		return fmt.Errorf(
			"decode %s response: %w",
			providerName,
			err,
		)
	}

	return nil
}

func ParseHTTPResponseError(
	response *http.Response,
	providerName string,
) error {
	data, _ := io.ReadAll(
		io.LimitReader(
			response.Body,
			32<<10,
		),
	)

	message := strings.TrimSpace(
		string(data),
	)

	if message == "" {
		message = http.StatusText(
			response.StatusCode,
		)
	}

	kind := ErrorUnknown

	switch response.StatusCode {
	case http.StatusUnauthorized,
		http.StatusForbidden:
		kind = ErrorUnauthorized

	case http.StatusTooManyRequests:
		kind = ErrorRateLimited

	case http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		kind = ErrorProviderUnavailable

	case http.StatusBadRequest,
		http.StatusUnprocessableEntity:
		kind = ErrorBadRequest
	}

	retryAfter := 0

	if value := response.Header.Get(
		"Retry-After",
	); value != "" {
		retryAfter, _ = strconv.Atoi(value)
	}

	return &ProviderError{
		Kind:       kind,
		Provider:   providerName,
		StatusCode: response.StatusCode,
		Message:    message,
		RetryAfter: retryAfter,
	}
}

func ReadSSE(
	ctx context.Context,
	response *http.Response,
	onData func(string) error,
) error {
	defer response.Body.Close()

	scanner := bufio.NewScanner(
		response.Body,
	)

	scanner.Buffer(
		make([]byte, 0, 64<<10),
		4<<20,
	)

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "data:") {
			if err := onData(
				strings.TrimSpace(
					strings.TrimPrefix(
						line,
						"data:",
					),
				),
			); err != nil {
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

package avalanche

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

func isRetryableHTTPStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable:
		return true
	default:
		return false
	}
}

func (c *DataAPIClient) doGET(
	ctx context.Context,
	endpoint string,
) (*http.Response, error) {
	if c == nil || c.httpClient == nil {
		return nil, fmt.Errorf(
			"Data API client is not initialized",
		)
	}

	totalAttempts := c.maxRetries + 1

	for attempt := 0; attempt < totalAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		request, err := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			endpoint,
			nil,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"create Data API request: %w",
				err,
			)
		}

		request.Header.Set("Accept", "application/json")

		if c.apiKey != "" {
			request.Header.Set(
				"x-glacier-api-key",
				c.apiKey,
			)
		}

		if err := c.reserveReadAttempt(); err != nil {
			return nil, err
		}

		response, requestErr := c.httpClient.Do(request)

		if requestErr == nil &&
			!isRetryableHTTPStatus(response.StatusCode) {
			return response, nil
		}

		lastAttempt := attempt == totalAttempts-1

		if requestErr == nil && lastAttempt {
			return response, nil
		}

		if requestErr != nil && lastAttempt {
			return nil, fmt.Errorf(
				"request Data API: %w",
				requestErr,
			)
		}

		if response != nil {
			_, _ = io.Copy(
				io.Discard,
				io.LimitReader(response.Body, 4<<10),
			)
			_ = response.Body.Close()
		}

		delay := c.retryDelay * time.Duration(1<<attempt)

		if delay <= 0 {
			continue
		}

		timer := time.NewTimer(delay)

		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}

			return nil, ctx.Err()

		case <-timer.C:
		}
	}

	return nil, fmt.Errorf(
		"Data API request exhausted retry attempts",
	)
}

package esi

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"time"

	esicore "eve-industry-planner/internal/core/esi"
	natscore "eve-industry-planner/internal/core/nats"
	rediscore "eve-industry-planner/internal/core/redis"
	"eve-industry-planner/internal/shared/logs"
	"eve-industry-planner/internal/shared/metrics"

	natslib "github.com/nats-io/nats.go"
	redislib "github.com/redis/go-redis/v9"
)

// ESIAdjustedPrice represents an individual adjusted price entry from ESI.
type ESIAdjustedPrice struct {
	TypeID        int32   `json:"type_id"`
	AdjustedPrice float64 `json:"adjusted_price"`
	AveragePrice  float64 `json:"average_price"`
}

// AdjustedPrice is the normalized structure used internally (only adjusted price per user request).
type AdjustedPrice struct {
	TypeID        int32   `json:"type_id"`
	AdjustedPrice float64 `json:"adjusted_price"`
	LastUpdated   int64   `json:"last_updated"`
}

// RefreshAdjustedPrices fetches the latest adjusted prices from ESI using a streaming decoder.
// It checks for HTTP 304 Not Modified responses to avoid unnecessary work when data hasn't changed.
// When data has changed, each item is persisted to Redis in the stream callback, and the ETag
// is saved after a successful pass. Cache headers are respected for scheduling future refreshes.
func RefreshAdjustedPrices(natsMessage MessageInterface, redisClient *redislib.Client, natsConn *natslib.Conn, esiClient esicore.ClientInterface) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	deliveryCount := uint64(0)
	if natsMessage != nil {
		deliveryCount = natsMessage.NumDelivered()
	}
	logs.Info("starting adjusted prices refresh", "delivery_count", deliveryCount)

	// Acquire a lock to prevent concurrent refreshes
	lockKey := "esi:market_prices:refresh_lock"
	lockAcquired, cleanup, err := rediscore.AcquireRefreshLock(ctx, redisClient, lockKey)
	if err != nil {
		logs.Warn("failed to acquire refresh lock, acknowledging message", "error", err, "delivery_count", deliveryCount)
		if natsMessage != nil {
			if ackErr := natsMessage.Ack(); ackErr != nil {
				logs.Warn("failed to ack message after lock error", "error", ackErr)
			} else {
				logs.Info("message acknowledged after lock acquisition error", "delivery_count", deliveryCount)
			}
		}
		return
	}
	if !lockAcquired {
		logs.Info("skipping refresh, another refresh in progress, acknowledging message", "delivery_count", deliveryCount)
		if natsMessage != nil {
			if ackErr := natsMessage.Ack(); ackErr != nil {
				logs.Warn("failed to ack message when lock already held", "error", ackErr)
			} else {
				logs.Info("message acknowledged (lock already held)", "delivery_count", deliveryCount)
			}
		}
		return
	}
	defer cleanup()

	// Read previous ETag from Redis (if available) to leverage 304s.
	prevETag, err := rediscore.GetMarketPricesETag(ctx, redisClient)
	if err != nil {
		logs.Warn("failed to get previous ETag", "error", err)
	}

	var count int
	lastProgress := time.Now()
	start := time.Now()
	logs.Info("adjusted prices refresh started", "etag_used", prevETag)
	// initial heartbeat so long fetches don't time out
	if natsMessage != nil {
		_ = natsMessage.InProgress()
	}

	var totalBytes int64
	var cacheSeconds int
	newETag, notModified, bytesRead, err := StreamAdjustedPrices(ctx, natsMessage, esiClient, prevETag, func(m AdjustedPrice) error {
		if err := rediscore.SaveMarketPrice(ctx, redisClient, m.TypeID, m); err != nil {
			return err
		}
		count++
		// send progress heartbeat at most every 5s
		if natsMessage != nil {
			if time.Since(lastProgress) >= 5*time.Second {
				_ = natsMessage.InProgress()
				lastProgress = time.Now()
			}
		}
		return nil
	}, &cacheSeconds)
	totalBytes = bytesRead
	if err != nil {
		logs.Debug("stream adjusted prices returned error", "error", err, "error_type", fmt.Sprintf("%T", err), "delivery_count", deliveryCount)

		// Check if this is a rate limit error
		if esicore.IsRateLimitError(err) {
			logs.Info("detected rate limit error in adjusted prices refresh", "error", err, "delivery_count", deliveryCount)
			rateLimitErr := esicore.GetRateLimitError(err)

			logs.Debug("rate limit error details",
				"retryable", rateLimitErr.Retryable,
				"retry_after", rateLimitErr.RetryAfter,
				"reason", rateLimitErr.Reason,
				"group", rateLimitErr.Group,
				"token_used", rateLimitErr.TokenUsed,
				"token_limit", rateLimitErr.TokenLimit,
				"estimated_tokens", rateLimitErr.EstimatedTokens,
				"delivery_count", deliveryCount)

			// Check if it's retryable
			if esicore.IsRetryableRateLimitError(err) {
				logs.Info("rate limit error is retryable, attempting NATS redelivery", "delivery_count", deliveryCount)
				if natsMessage != nil {
					waitDuration := time.Until(rateLimitErr.RetryAfter)
					now := time.Now()

					logs.Debug("calculating wait duration for redelivery",
						"now", now,
						"retry_after", rateLimitErr.RetryAfter,
						"wait_duration", waitDuration,
						"wait_duration_seconds", waitDuration.Seconds(),
						"delivery_count", deliveryCount)

					if waitDuration > 0 {
						logs.Info("adjusted prices refresh rate limited, delaying redelivery",
							"retry_after", rateLimitErr.RetryAfter,
							"wait_duration", waitDuration,
							"wait_duration_seconds", waitDuration.Seconds(),
							"wait_duration_minutes", waitDuration.Minutes(),
							"reason", rateLimitErr.Reason,
							"group", rateLimitErr.Group,
							"token_used", rateLimitErr.TokenUsed,
							"token_limit", rateLimitErr.TokenLimit,
							"estimated_tokens", rateLimitErr.EstimatedTokens,
							"delivery_count", deliveryCount)

						logs.Debug("calling NakWithDelay", "delay", waitDuration, "delivery_count", deliveryCount)
						if nakErr := natsMessage.NakWithDelay(waitDuration); nakErr != nil {
							logs.Error("failed to nack with delay, falling back to normal nack",
								"nak_error", nakErr,
								"nak_error_type", fmt.Sprintf("%T", nakErr),
								"requested_delay", waitDuration,
								"delivery_count", deliveryCount)
							// Fall back to normal nack
							logs.Warn("falling back to NackWithBackoff", "delivery_count", deliveryCount)
							natscore.NackWithBackoff(natsMessage)
						} else {
							logs.Info("successfully called NakWithDelay, message will be redelivered after delay",
								"delay", waitDuration,
								"redelivery_time", rateLimitErr.RetryAfter,
								"delivery_count", deliveryCount)
						}
						return
					} else {
						logs.Warn("wait duration is <= 0, cannot delay redelivery, falling back to normal nack",
							"wait_duration", waitDuration,
							"retry_after", rateLimitErr.RetryAfter,
							"now", now,
							"delivery_count", deliveryCount)
						natscore.NackWithBackoff(natsMessage)
						return
					}
				} else {
					logs.Warn("rate limit error is retryable but natsMessage is nil, cannot delay redelivery",
						"delivery_count", deliveryCount)
				}
			} else {
				logs.Warn("rate limit error is NOT retryable, using normal nack backoff",
					"reason", rateLimitErr.Reason,
					"group", rateLimitErr.Group,
					"delivery_count", deliveryCount)
				if natsMessage != nil {
					natscore.NackWithBackoff(natsMessage)
				}
				return
			}
		}

		logs.Error("failed streaming ESI adjusted prices, nacking with backoff",
			"error", err,
			"error_type", fmt.Sprintf("%T", err),
			"reason", "stream_error",
			"delivery_count", deliveryCount)
		if natsMessage != nil {
			natscore.NackWithBackoff(natsMessage)
		}
		metrics.GetESIMarketPrices().Errors.WithLabelValues("stream").Inc()
		return
	}

	if notModified {
		logs.Info("ESI adjusted prices not modified (ETag match), acknowledging message", "etag_new", newETag, "delivery_count", deliveryCount)
		if natsMessage != nil {
			if ackErr := natsMessage.Ack(); ackErr != nil {
				logs.Warn("failed to ack message (not modified)", "error", ackErr)
			} else {
				logs.Info("message acknowledged (not modified)", "delivery_count", deliveryCount)
			}
		}
		m := metrics.GetESIMarketPrices()
		m.Requests.Observe(time.Since(start).Seconds())
		m.Bytes.Add(float64(totalBytes))
		// Update metrics if cache headers available (for monitoring)
		if cacheSeconds > 0 {
			nextRefreshMillis := time.Now().Add(time.Duration(cacheSeconds) * time.Second).UnixMilli()
			metrics.GetESIMarketPrices().NextRefresh.Set(float64(nextRefreshMillis))
		}
		return
	}

	if err := rediscore.SaveMarketPricesETag(ctx, redisClient, newETag); err != nil {
		logs.Error("failed to save ETag, nacking with backoff", "error", err, "reason", "etag_save_error", "delivery_count", deliveryCount)
		if natsMessage != nil {
			natscore.NackWithBackoff(natsMessage)
		}
		metrics.GetESIMarketPrices().Errors.WithLabelValues("etag_save").Inc()
		return
	}

	// Save last updated timestamp
	if err := rediscore.SaveMarketPricesLastUpdated(ctx, redisClient, time.Now().UnixMilli()); err != nil {
		logs.Warn("failed to save last updated timestamp, nacking with backoff", "error", err, "reason", "last_updated_save_error", "delivery_count", deliveryCount)
		if natsMessage != nil {
			natscore.NackWithBackoff(natsMessage)
		}
		metrics.GetESIMarketPrices().Errors.WithLabelValues("last_updated_save").Inc()
		return
	}

	// Update metrics if cache headers available (for monitoring)
	if cacheSeconds > 0 {
		nextRefreshMillis := time.Now().Add(time.Duration(cacheSeconds) * time.Second).UnixMilli()
		metrics.GetESIMarketPrices().NextRefresh.Set(float64(nextRefreshMillis))
	}

	// Acknowledge message completion
	if natsMessage != nil {
		if ackErr := natsMessage.Ack(); ackErr != nil {
			logs.Warn("failed to ack message (success)", "error", ackErr, "delivery_count", deliveryCount)
		} else {
			logs.Info("message acknowledged (success)", "delivery_count", deliveryCount)
		}
	}

	duration := time.Since(start)
	m := metrics.GetESIMarketPrices()
	m.Requests.Observe(duration.Seconds())
	m.Bytes.Add(float64(totalBytes))
	m.Items.Add(float64(count))
	m.LastUpdated.Set(float64(time.Now().UnixMilli()))
	logs.Info("updated ESI adjusted prices", "count", count, "etag_new", newETag, "bytes_read", totalBytes, "duration_ms", duration.Milliseconds())
}

// StreamAdjustedPrices makes an HTTP request to ESI and checks the response status code first.
// For HTTP 304 Not Modified responses, it returns early without streaming.
// For HTTP 200 OK responses, it performs a streaming decode of the ESI array and invokes
// onItem for each normalized AdjustedPrice. Callers typically persist within the callback.
// Returns the new ETag, whether it was not modified (HTTP 304), bytes read, and any error.
// cacheSecondsOut will be populated with parsed cache max-age from response headers if available.
func StreamAdjustedPrices(ctx context.Context, natsMessage MessageInterface, esiClient esicore.ClientInterface, etag string, onItem func(AdjustedPrice) error, cacheSecondsOut *int) (string, bool, int64, error) {
	if esiClient == nil {
		return "", false, 0, errors.New("ESI client is nil")
	}
	if onItem == nil {
		return "", false, 0, errors.New("onItem callback is nil")
	}

	path := "/v1/markets/prices/"
	headers := map[string]string{
		"Accept":          "application/json",
		"Accept-Encoding": "gzip",
	}
	if etag != "" {
		headers["If-None-Match"] = etag
	}

	// Retry on transient errors with exponential backoff + jitter
	// Check for rate limit errors and reschedule if retryable
	var resp *http.Response
	maxAttempts := 4
	var err error

	logs.Debug("starting ESI request loop for adjusted prices", "path", path, "etag_provided", etag != "", "max_attempts", maxAttempts)

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		logs.Debug("making ESI request attempt", "attempt", attempt, "max_attempts", maxAttempts, "path", path)
		resp, err = esiClient.DoRequest(ctx, http.MethodGet, path, headers)
		if err == nil {
			logs.Debug("ESI request succeeded", "attempt", attempt, "path", path)
			break
		}

		logs.Debug("ESI request failed",
			"attempt", attempt,
			"max_attempts", maxAttempts,
			"error", err,
			"error_type", fmt.Sprintf("%T", err),
			"path", path)

		// Check if this is a rate limit error
		if esicore.IsRateLimitError(err) {
			rateLimitErr := esicore.GetRateLimitError(err)
			logs.Info("rate limit error detected in stream function, returning for NATS redelivery",
				"attempt", attempt,
				"retryable", rateLimitErr.Retryable,
				"retry_after", rateLimitErr.RetryAfter,
				"reason", rateLimitErr.Reason,
				"group", rateLimitErr.Group,
				"token_used", rateLimitErr.TokenUsed,
				"token_limit", rateLimitErr.TokenLimit,
				"estimated_tokens", rateLimitErr.EstimatedTokens,
				"path", path)

			// Check if this is a retryable rate limit error - return it for NATS redelivery handling
			if esicore.IsRetryableRateLimitError(err) {
				logs.Debug("rate limit error is retryable, returning error for caller to handle redelivery", "path", path)
				// Return the error so caller can handle NATS redelivery with delay
				return "", false, 0, err
			} else {
				logs.Warn("rate limit error is NOT retryable, returning error anyway", "path", path, "reason", rateLimitErr.Reason)
				return "", false, 0, err
			}
		}

		if attempt >= maxAttempts {
			logs.Debug("max attempts reached, returning error", "attempt", attempt, "max_attempts", maxAttempts, "error", err)
			return "", false, 0, err
		}
		// Exponential backoff: 500ms, 1s, 2s
		backoff := time.Duration(500*(1<<uint(attempt-1))) * time.Millisecond
		// Jitter: random 0-100ms
		jitter := time.Duration(rand.Intn(100)) * time.Millisecond
		waitTime := backoff + jitter
		logs.Debug("waiting before retry with exponential backoff", "attempt", attempt, "backoff", backoff, "jitter", jitter, "wait_time", waitTime)
		time.Sleep(waitTime)
	}
	if resp != nil {
		defer resp.Body.Close()
	}
	if resp == nil {
		return "", false, 0, errors.New("nil HTTP response")
	}

	// Check response status code first to avoid unnecessary streaming
	if resp.StatusCode == http.StatusNotModified {
		newETag := resp.Header.Get("ETag")
		if cacheSecondsOut != nil {
			*cacheSecondsOut = parseCacheSeconds(resp)
		}
		return newETag, true, 0, nil
	}
	if resp.StatusCode != http.StatusOK {
		newETag := resp.Header.Get("ETag")
		body, _ := io.ReadAll(resp.Body)
		return newETag, false, 0, errors.New(string(body))
	}

	// Extract ETag for successful responses
	newETag := resp.Header.Get("ETag")

	if cacheSecondsOut != nil {
		*cacheSecondsOut = parseCacheSeconds(resp)
	}

	// Handle gzip decompression if needed
	bodyReader := resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gzReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return newETag, false, 0, err
		}
		bodyReader = gzReader
		defer gzReader.Close()
	}

	// Count bytes as we decode
	cr := &countingReader{r: bodyReader}
	dec := json.NewDecoder(cr)
	// Expect start of array
	tok, err := dec.Token()
	if err != nil {
		return newETag, false, cr.n, err
	}
	del, ok := tok.(json.Delim)
	if !ok || del != '[' {
		return newETag, false, cr.n, errors.New("invalid JSON: expected array start")
	}

	nowMs := time.Now().UnixMilli()
	for dec.More() {
		var item ESIAdjustedPrice
		if err := dec.Decode(&item); err != nil {
			return newETag, false, cr.n, err
		}
		// Only save adjusted_price as per user request
		m := AdjustedPrice{
			TypeID:        item.TypeID,
			AdjustedPrice: item.AdjustedPrice,
			LastUpdated:   nowMs,
		}
		if err := onItem(m); err != nil {
			return newETag, false, cr.n, err
		}
	}
	// Consume end of array
	if _, err := dec.Token(); err != nil {
		return newETag, false, cr.n, err
	}

	return newETag, false, cr.n, nil
}

package esi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"eve-industry-planner/internal/shared/httpclient"
	"eve-industry-planner/internal/shared/logs"

	"golang.org/x/time/rate"
)

// RateLimitError represents a rate limit error with classification for task handling
type RateLimitError struct {
	// Retryable indicates if this error should trigger a retry
	Retryable bool
	// RetryAfter is the time when retry should be attempted (if Retryable is true)
	RetryAfter time.Time
	// Reason describes the reason for rate limiting
	Reason string
	// Group is the rate limit group name
	Group string
	// TokenUsed is the current tokens used
	TokenUsed int
	// TokenLimit is the token limit
	TokenLimit int
	// EstimatedTokens is the tokens needed for the request
	EstimatedTokens int
}

func (e *RateLimitError) Error() string {
	if e.Retryable {
		waitTime := time.Until(e.RetryAfter)
		return fmt.Sprintf("rate limit %s (retryable, retry after %v, waiting %v)", e.Reason, e.RetryAfter, waitTime)
	}
	return fmt.Sprintf("rate limit %s (non-retryable)", e.Reason)
}

// IsRetryableRateLimitError checks if an error is a retryable rate limit error
func IsRetryableRateLimitError(err error) bool {
	var rateLimitErr *RateLimitError
	return errors.As(err, &rateLimitErr) && rateLimitErr.Retryable
}

// IsRateLimitError checks if an error is any rate limit error
func IsRateLimitError(err error) bool {
	var rateLimitErr *RateLimitError
	return errors.As(err, &rateLimitErr)
}

// GetRateLimitError extracts RateLimitError from an error if present
func GetRateLimitError(err error) *RateLimitError {
	var rateLimitErr *RateLimitError
	if errors.As(err, &rateLimitErr) {
		return rateLimitErr
	}
	return nil
}

// TokenConsumption tracks token consumption with timestamps for floating window
type TokenConsumption struct {
	Tokens   int
	Consumed time.Time
}

// GroupLimiter holds a limiter and token bucket tracking for ESI rate limiting.
type GroupLimiter struct {
	// Rate limiter for general throttling
	Limiter      *rate.Limiter
	DefaultRate  rate.Limit
	DefaultBurst int
	Name         string

	// Token bucket tracking (floating window)
	mu             sync.RWMutex
	tokenLimit     int                // Total tokens allowed (from X-Ratelimit-Limit)
	tokenUsed      int                // Current tokens used
	consumptions   []TokenConsumption // History for floating window (15 min)
	lastUpdate     time.Time
	retryAfter     time.Time     // When to retry after 429
	windowDuration time.Duration // Token return window (15 minutes)
}

// ESIClient manages API requests and per-group dynamic rate limiting.
type ESIClient struct {
	httpClient *http.Client
	baseURL    string
	mu         sync.RWMutex
	limiters   map[string]*GroupLimiter // Key: group name from X-Ratelimit-Group header
	defLim     *GroupLimiter
	// Tracks which paths belong to which groups (discovered dynamically)
	pathToGroup map[string]string
	// Synchronization for unknown groups - prevents concurrent requests to same unknown group
	unknownGroupMutex sync.Mutex
	unknownGroups     map[string]*sync.Mutex // Per-path mutex for unknown groups
	// Test mode configuration
	testMode           bool
	testModePath       string        // Path pattern to match for test mode (empty = all paths)
	testModeScenario   string        // Test scenario: "retryable_short", "retryable_long", "non_retryable", "429_response"
	testModeRetryAfter time.Duration // For retryable scenarios
}

// NewESIClient initializes the client with a default limiter.
func NewESIClient(baseURL string, defaultRPS float64, burst int) *ESIClient {
	def := &GroupLimiter{
		Limiter:        rate.NewLimiter(rate.Limit(defaultRPS), burst),
		DefaultRate:    rate.Limit(defaultRPS),
		DefaultBurst:   burst,
		Name:           "default",
		tokenLimit:     1000, // Default token limit (will be updated from headers)
		consumptions:   make([]TokenConsumption, 0),
		windowDuration: 15 * time.Minute,
		lastUpdate:     time.Now(),
	}

	// Check for test mode environment variables
	testMode := os.Getenv("ESI_TEST_MODE") == "true"
	testModePath := os.Getenv("ESI_TEST_MODE_PATH")         // e.g., "/v1/markets/prices/" or "/v1/industry/systems/"
	testModeScenario := os.Getenv("ESI_TEST_MODE_SCENARIO") // "retryable_short", "retryable_long", "non_retryable", "429_response"
	testModeRetryAfter := 1 * time.Minute                   // Default short retry

	if testMode {
		// Parse retry after duration from env var if provided
		if retryAfterStr := os.Getenv("ESI_TEST_MODE_RETRY_AFTER"); retryAfterStr != "" {
			if duration, err := time.ParseDuration(retryAfterStr); err == nil {
				testModeRetryAfter = duration
			}
		}

		logs.Warn("ESI TEST MODE ENABLED",
			"test_mode", true,
			"test_path", testModePath,
			"test_scenario", testModeScenario,
			"test_retry_after", testModeRetryAfter,
			"warning", "This will inject fake rate limit errors for testing!")
	}

	return &ESIClient{
		httpClient: &http.Client{
			Transport: tunedTransport(),
			Timeout:   30 * time.Second,
		},
		baseURL:            baseURL,
		limiters:           make(map[string]*GroupLimiter),
		defLim:             def,
		pathToGroup:        make(map[string]string),
		unknownGroups:      make(map[string]*sync.Mutex),
		testMode:           testMode,
		testModePath:       testModePath,
		testModeScenario:   testModeScenario,
		testModeRetryAfter: testModeRetryAfter,
	}
}

// tunedTransport returns a configured HTTP transport matching the existing configuration
func tunedTransport() *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
		DisableCompression:    false,
	}
}

// AddGroupLimiter manually registers a limiter for a specific group name (optional, for explicit control).
// Groups are typically discovered automatically from X-Ratelimit-Group headers.
func (c *ESIClient) AddGroupLimiter(groupName string, rps float64, burst int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.limiters[groupName] = &GroupLimiter{
		Limiter:        rate.NewLimiter(rate.Limit(rps), burst),
		DefaultRate:    rate.Limit(rps),
		DefaultBurst:   burst,
		Name:           groupName,
		tokenLimit:     1000, // Default token limit (will be updated from headers)
		consumptions:   make([]TokenConsumption, 0),
		windowDuration: 15 * time.Minute,
		lastUpdate:     time.Now(),
	}
	logs.Info("manually added group limiter", "group", groupName, "rps", rps, "burst", burst)
}

// getLimiterForPath finds the limiter for a given path by checking if we know its group.
// Returns the default limiter if the group is unknown (will be discovered from response headers).
func (c *ESIClient) getLimiterForPath(path string) (*GroupLimiter, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Check if we know which group this path belongs to
	if groupName, exists := c.pathToGroup[path]; exists {
		if limiter, hasLimiter := c.limiters[groupName]; hasLimiter {
			logs.Debug("using known group limiter",
				"path", path,
				"group", groupName,
				"token_limit", limiter.tokenLimit,
				"token_used", limiter.tokenUsed)
			return limiter, true
		}
	}

	// Unknown group - return default limiter
	// The default limiter provides steady request rate limiting for paths without group headers
	logs.Debug("path group unknown, using default limiter with steady rate",
		"path", path,
		"default_group", c.defLim.Name,
		"default_rate", fmt.Sprintf("%.2f req/s", c.defLim.DefaultRate),
		"default_burst", c.defLim.DefaultBurst,
		"default_token_limit", c.defLim.tokenLimit,
		"note", "default limiter enforces steady request rate for unknown paths")
	return c.defLim, false
}

// getOrCreateGroupLimiter gets an existing group limiter or creates a new one.
// This is called after receiving a response with X-Ratelimit-Group header.
func (c *ESIClient) getOrCreateGroupLimiter(groupName string, path string, tokenLimit int) *GroupLimiter {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if limiter already exists
	if limiter, exists := c.limiters[groupName]; exists {
		// Update token limit if provided and different
		if tokenLimit > 0 && tokenLimit != limiter.tokenLimit {
			limiter.mu.Lock()
			oldLimit := limiter.tokenLimit
			limiter.tokenLimit = tokenLimit
			limiter.mu.Unlock()
			logs.Info("updated token limit for existing group",
				"group", groupName,
				"path", path,
				"old_limit", oldLimit,
				"new_limit", tokenLimit)
		}
		// Map this path to the group (may be updating or adding new path to same group)
		oldPath, hadPath := c.pathToGroup[path]
		c.pathToGroup[path] = groupName

		if hadPath && oldPath != groupName {
			logs.Info("path reassigned to different group",
				"path", path,
				"old_group", oldPath,
				"new_group", groupName)
		} else if !hadPath {
			logs.Debug("mapped path to existing group",
				"path", path,
				"group", groupName,
				"token_limit", limiter.tokenLimit,
				"token_used", limiter.tokenUsed)
		}
		return limiter
	}

	// Create new limiter for this group
	// Use default rate/burst initially - can be tuned later if needed
	newLimiter := &GroupLimiter{
		Limiter:        rate.NewLimiter(c.defLim.DefaultRate, c.defLim.DefaultBurst),
		DefaultRate:    c.defLim.DefaultRate,
		DefaultBurst:   c.defLim.DefaultBurst,
		Name:           groupName,
		tokenLimit:     tokenLimit,
		consumptions:   make([]TokenConsumption, 0),
		windowDuration: 15 * time.Minute,
		lastUpdate:     time.Now(),
	}

	if newLimiter.tokenLimit == 0 {
		newLimiter.tokenLimit = 1000 // Default if not provided
		logs.Debug("using default token limit for new group",
			"group", groupName,
			"default_limit", newLimiter.tokenLimit)
	}

	c.limiters[groupName] = newLimiter
	c.pathToGroup[path] = groupName

	logs.Info("discovered new ESI rate limit group",
		"group", groupName,
		"path", path,
		"token_limit", newLimiter.tokenLimit,
		"default_rate", fmt.Sprintf("%.2f req/s", c.defLim.DefaultRate),
		"default_burst", c.defLim.DefaultBurst,
		"total_groups", len(c.limiters))

	return newLimiter
}

// getMutexForUnknownPath gets or creates a mutex for an unknown path to prevent concurrent discovery.
func (c *ESIClient) getMutexForUnknownPath(path string) *sync.Mutex {
	c.unknownGroupMutex.Lock()
	defer c.unknownGroupMutex.Unlock()

	if mutex, exists := c.unknownGroups[path]; exists {
		logs.Debug("reusing mutex for unknown path",
			"path", path,
			"waiting_paths", len(c.unknownGroups))
		return mutex
	}

	mutex := &sync.Mutex{}
	c.unknownGroups[path] = mutex
	logs.Debug("created new mutex for unknown path",
		"path", path,
		"total_unknown_paths", len(c.unknownGroups))
	return mutex
}

// getTokensForStatus returns token cost based on HTTP status code per ESI rules
func getTokensForStatus(statusCode int) int {
	switch {
	case statusCode >= 200 && statusCode < 300:
		return 2 // 2XX responses cost 2 tokens
	case statusCode >= 300 && statusCode < 400:
		return 1 // 3XX responses cost 1 token
	case statusCode >= 400 && statusCode < 500:
		return 5 // 4XX responses cost 5 tokens
	case statusCode >= 500:
		return 0 // 5XX responses cost 0 tokens
	default:
		return 2 // Default to 2 for unknown status codes
	}
}

// cleanupOldConsumptions removes token consumptions older than the window duration
func (gl *GroupLimiter) cleanupOldConsumptions() {
	now := time.Now()
	cutoff := now.Add(-gl.windowDuration)

	gl.mu.Lock()
	defer gl.mu.Unlock()

	beforeCount := len(gl.consumptions)
	beforeTokens := gl.tokenUsed

	// Remove consumptions older than the window
	validConsumptions := make([]TokenConsumption, 0, len(gl.consumptions))
	totalTokens := 0
	expiredTokens := 0
	for _, cons := range gl.consumptions {
		if cons.Consumed.After(cutoff) {
			validConsumptions = append(validConsumptions, cons)
			totalTokens += cons.Tokens
		} else {
			expiredTokens += cons.Tokens
		}
	}
	gl.consumptions = validConsumptions
	gl.tokenUsed = totalTokens
	gl.lastUpdate = now

	if expiredTokens > 0 || beforeCount != len(validConsumptions) {
		logs.Debug("cleaned up old token consumptions",
			"group", gl.Name,
			"before_count", beforeCount,
			"after_count", len(validConsumptions),
			"before_tokens", beforeTokens,
			"after_tokens", totalTokens,
			"expired_tokens", expiredTokens,
			"window_duration", gl.windowDuration)
	}
}

// updateFromHeaders updates token bucket from ESI rate limit headers.
// If this is a new group discovery, creates the group limiter and maps the path.
func (c *ESIClient) updateFromHeaders(gl *GroupLimiter, resp *http.Response, tokensConsumed int, path string) *GroupLimiter {
	now := time.Now()

	// Parse ESI rate limit headers
	limitStr := resp.Header.Get("X-Ratelimit-Limit")
	remainingStr := resp.Header.Get("X-Ratelimit-Remaining")
	usedStr := resp.Header.Get("X-Ratelimit-Used")
	groupStr := resp.Header.Get("X-Ratelimit-Group")

	// Determine token limit
	tokenLimit := 0
	if limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil {
			tokenLimit = limit
		}
	}

	// If we have a group name and this isn't the default limiter (or default doesn't match),
	// check if we need to create/switch to the correct group limiter
	var actualLimiter *GroupLimiter
	if groupStr != "" {
		// Check if we already know this path's group
		c.mu.RLock()
		knownGroup, pathKnown := c.pathToGroup[path]
		knownLimiter, hasLimiter := c.limiters[groupStr]
		c.mu.RUnlock()

		if pathKnown && knownGroup == groupStr && hasLimiter {
			// We know this path belongs to this group and limiter exists
			actualLimiter = knownLimiter
		} else if groupStr != gl.Name || !hasLimiter {
			// This is a new group discovery or group name mismatch
			actualLimiter = c.getOrCreateGroupLimiter(groupStr, path, tokenLimit)
		} else {
			// Same group, use existing limiter
			actualLimiter = gl
		}
	} else {
		// No group header - use the limiter we used for the request (default limiter)
		// This happens when:
		// 1. The route hasn't had rate limiting rolled out yet (pre-rollout)
		// 2. ESI server isn't sending group headers for some reason
		// 3. We're hitting an old endpoint that doesn't support grouping
		//
		// Behavior: All paths without group headers use the default limiter which has
		// a steady request rate limit. This provides basic rate limiting while we wait
		// for ESI to provide group information.
		actualLimiter = gl

		// Log this scenario so we can track which paths don't have group headers
		logs.Debug("response missing X-Ratelimit-Group header, using default limiter",
			"path", path,
			"status", resp.StatusCode,
			"limiter", gl.Name,
			"limiter_rate", fmt.Sprintf("%.2f req/s", gl.Limiter.Limit()),
			"has_limit_header", limitStr != "",
			"has_remaining_header", remainingStr != "",
			"has_used_header", usedStr != "",
			"note", "default limiter provides steady rate limiting for paths without group headers")

		// Note: We intentionally don't map the path to any group, so future requests will continue
		// using the default limiter. This means:
		// - Unknown paths without headers will stay on default limiter (steady rate)
		// - Multiple paths without headers will share the default limiter's token bucket
		// - The default limiter has a steady request rate (RPS limit) that prevents bursts
		// - This is the desired behavior for pre-rollout and unknown endpoints
	}

	// Clean up old consumptions first
	actualLimiter.cleanupOldConsumptions()

	actualLimiter.mu.Lock()
	defer actualLimiter.mu.Unlock()

	// Update token limit if provided and different
	if tokenLimit > 0 && tokenLimit != actualLimiter.tokenLimit {
		oldLimit := actualLimiter.tokenLimit
		actualLimiter.tokenLimit = tokenLimit
		logs.Debug("updated token limit from response header",
			"limiter", actualLimiter.Name,
			"path", path,
			"old_limit", oldLimit,
			"new_limit", tokenLimit)
	}

	// Use server-provided remaining if available, otherwise calculate from consumption
	oldTokenUsed := actualLimiter.tokenUsed
	if remainingStr != "" {
		if remaining, err := strconv.Atoi(remainingStr); err == nil {
			// Server knows best - use their count
			if usedStr != "" {
				if used, err := strconv.Atoi(usedStr); err == nil {
					actualLimiter.tokenUsed = used
					logs.Debug("updated token usage from X-Ratelimit-Used header",
						"limiter", actualLimiter.Name,
						"path", path,
						"old_used", oldTokenUsed,
						"new_used", used,
						"remaining", remaining,
						"limit", actualLimiter.tokenLimit)
				}
			} else {
				newUsed := actualLimiter.tokenLimit - remaining
				actualLimiter.tokenUsed = newUsed
				logs.Debug("calculated token usage from X-Ratelimit-Remaining",
					"limiter", actualLimiter.Name,
					"path", path,
					"old_used", oldTokenUsed,
					"new_used", newUsed,
					"remaining", remaining,
					"limit", actualLimiter.tokenLimit)
			}
		}
	} else {
		// Track consumption locally (no server headers available)
		beforeCount := len(actualLimiter.consumptions)
		actualLimiter.consumptions = append(actualLimiter.consumptions, TokenConsumption{
			Tokens:   tokensConsumed,
			Consumed: now,
		})
		actualLimiter.tokenUsed += tokensConsumed

		logs.Debug("tracked token consumption locally (no server headers)",
			"limiter", actualLimiter.Name,
			"path", path,
			"old_used", oldTokenUsed,
			"new_used", actualLimiter.tokenUsed,
			"tokens_consumed", tokensConsumed,
			"consumption_count", len(actualLimiter.consumptions),
			"consumption_history_added", len(actualLimiter.consumptions)-beforeCount)
	}

	// Handle 429 responses with Retry-After
	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfterStr := resp.Header.Get("Retry-After")
		if retryAfterStr != "" {
			if seconds, err := strconv.Atoi(retryAfterStr); err == nil {
				actualLimiter.retryAfter = now.Add(time.Duration(seconds) * time.Second)
				logs.Warn("ESI rate limit exceeded, waiting for retry",
					"group", groupStr,
					"retry_after_seconds", seconds,
					"limit", actualLimiter.tokenLimit,
					"used", actualLimiter.tokenUsed)
			}
		} else {
			// Default to window duration if no Retry-After
			actualLimiter.retryAfter = now.Add(actualLimiter.windowDuration)
		}
	}

	// Log rate limit status periodically
	if remainingStr != "" {
		if remaining, err := strconv.Atoi(remainingStr); err == nil {
			remainingPercent := float64(remaining) / float64(actualLimiter.tokenLimit) * 100
			if remainingPercent < 10 {
				logs.Warn("ESI rate limit low",
					"group", groupStr,
					"limiter", actualLimiter.Name,
					"path", path,
					"remaining", remaining,
					"limit", actualLimiter.tokenLimit,
					"used", actualLimiter.tokenUsed,
					"percent", fmt.Sprintf("%.1f%%", remainingPercent))
			} else if remainingPercent < 25 {
				// Log at debug level for 10-25% remaining
				logs.Debug("ESI rate limit moderate",
					"group", groupStr,
					"limiter", actualLimiter.Name,
					"path", path,
					"remaining", remaining,
					"limit", actualLimiter.tokenLimit,
					"used", actualLimiter.tokenUsed,
					"percent", fmt.Sprintf("%.1f%%", remainingPercent))
			}
		}
	} else if groupStr == "" {
		// Log when we have no group header AND no remaining header - helps identify problematic endpoints
		logs.Debug("response missing rate limit headers",
			"path", path,
			"status", resp.StatusCode,
			"limiter", actualLimiter.Name,
			"token_used", actualLimiter.tokenUsed,
			"token_limit", actualLimiter.tokenLimit,
			"note", "no X-Ratelimit-Group or X-Ratelimit-Remaining headers")
	}

	return actualLimiter
}

// canMakeRequest checks if we have enough tokens and aren't in a retry-after period
// Returns a classified RateLimitError if rate limited, nil otherwise
func (gl *GroupLimiter) canMakeRequest(ctx context.Context, estimatedTokens int) error {
	// Clean up expired tokens first to ensure accurate token count
	// This proactively returns expired tokens to the bucket (15-minute floating window)
	gl.cleanupOldConsumptions()

	gl.mu.RLock()
	retryAfter := gl.retryAfter
	tokenUsed := gl.tokenUsed
	tokenLimit := gl.tokenLimit
	gl.mu.RUnlock()

	now := time.Now()

	// Check if we're in retry-after period (from previous 429 response)
	if now.Before(retryAfter) {
		waitTime := time.Until(retryAfter)
		logs.Debug("request blocked by retry-after",
			"group", gl.Name,
			"retry_after", retryAfter,
			"wait_time", waitTime,
			"estimated_tokens", estimatedTokens)
		return &RateLimitError{
			Retryable:       true,
			RetryAfter:      retryAfter,
			Reason:          "retry-after period active",
			Group:           gl.Name,
			TokenUsed:       tokenUsed,
			TokenLimit:      tokenLimit,
			EstimatedTokens: estimatedTokens,
		}
	}

	// Check if we have enough tokens (after cleanup)
	remaining := tokenLimit - tokenUsed
	if tokenUsed+estimatedTokens > tokenLimit {
		// Check if tokens will become available soon (within the next minute)
		// by looking at oldest consumption
		gl.mu.RLock()
		oldestExpiry := now.Add(gl.windowDuration)
		if len(gl.consumptions) > 0 {
			oldestConsumption := gl.consumptions[0]
			for _, cons := range gl.consumptions {
				if cons.Consumed.Before(oldestConsumption.Consumed) {
					oldestConsumption = cons
				}
			}
			oldestExpiry = oldestConsumption.Consumed.Add(gl.windowDuration)
		}
		gl.mu.RUnlock()

		// If tokens will become available soon, mark as retryable
		retryable := false
		retryAfterTime := oldestExpiry
		if now.Before(oldestExpiry) && time.Until(oldestExpiry) < 5*time.Minute {
			retryable = true
		}

		logs.Warn("insufficient tokens after cleanup",
			"group", gl.Name,
			"token_used", tokenUsed,
			"token_limit", tokenLimit,
			"estimated_tokens", estimatedTokens,
			"remaining", remaining,
			"needed", estimatedTokens,
			"retryable", retryable,
			"retry_after", retryAfterTime)

		return &RateLimitError{
			Retryable:       retryable,
			RetryAfter:      retryAfterTime,
			Reason:          "insufficient tokens",
			Group:           gl.Name,
			TokenUsed:       tokenUsed,
			TokenLimit:      tokenLimit,
			EstimatedTokens: estimatedTokens,
		}
	}

	logs.Debug("token check passed",
		"group", gl.Name,
		"token_used", tokenUsed,
		"token_limit", tokenLimit,
		"remaining", remaining,
		"estimated_tokens", estimatedTokens)

	return nil
}

// Do performs a rate-limited HTTP request for the given endpoint.
// Returns the response body, status code, headers, and any error.
func (c *ESIClient) Do(ctx context.Context, method, path string, headers map[string]string) ([]byte, *http.Response, error) {
	logs.Debug("ESI request initiated",
		"method", method,
		"path", path)

	// Get limiter for this path (or default if unknown)
	gl, known := c.getLimiterForPath(path)

	// If group is unknown, use mutex to prevent concurrent requests to same unknown path
	var pathMutex *sync.Mutex
	if !known {
		logs.Info("acquiring mutex for unknown path discovery",
			"path", path,
			"group", gl.Name)
		pathMutex = c.getMutexForUnknownPath(path)
		pathMutex.Lock()
		defer func() {
			pathMutex.Unlock()
			logs.Debug("released mutex for path",
				"path", path)
		}()
	}

	// Estimate tokens needed (assume 2XX response = 2 tokens for now)
	estimatedTokens := 2
	logs.Debug("estimated token cost",
		"path", path,
		"group", gl.Name,
		"estimated_tokens", estimatedTokens)

	// TEST MODE: Inject rate limit errors before making actual request
	if c.testMode && c.shouldInjectTestError(path) {
		testErr := c.injectTestRateLimitError(gl, estimatedTokens)
		if testErr != nil {
			logs.Warn("TEST MODE: Injecting rate limit error",
				"path", path,
				"group", gl.Name,
				"scenario", c.testModeScenario,
				"error", testErr)
			return nil, nil, testErr
		}
	}

	// Check if we can make the request (token bucket + retry-after)
	// Return immediately with classified error if rate limited - let task decide how to handle
	if err := gl.canMakeRequest(ctx, estimatedTokens); err != nil {
		logs.Debug("rate limit check failed, returning classified error",
			"path", path,
			"group", gl.Name,
			"error", err)
		return nil, nil, err
	}

	// Wait for general rate limiter (prevents burst)
	rateWaitStart := time.Now()
	if err := gl.Limiter.Wait(ctx); err != nil {
		logs.Error("rate limiter wait failed",
			"path", path,
			"group", gl.Name,
			"error", err)
		return nil, nil, fmt.Errorf("rate wait failed: %w", err)
	}
	rateWaitDuration := time.Since(rateWaitStart)
	if rateWaitDuration > 100*time.Millisecond {
		logs.Debug("rate limiter wait duration",
			"path", path,
			"group", gl.Name,
			"		wait_duration", rateWaitDuration)
	}

	logs.Debug("making HTTP request",
		"method", method,
		"path", path,
		"group", gl.Name,
		"url", c.baseURL+path)

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	if err != nil {
		return nil, nil, err
	}

	// Apply default headers
	httpclient.ApplyDefaultHeaders(req)

	// Apply custom headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	requestStart := time.Now()
	resp, err := c.httpClient.Do(req)
	requestDuration := time.Since(requestStart)
	if err != nil {
		logs.Error("HTTP request failed",
			"path", path,
			"group", gl.Name,
			"error", err,
			"duration", requestDuration)
		return nil, nil, err
	}

	// Calculate actual tokens consumed
	tokensConsumed := getTokensForStatus(resp.StatusCode)
	logs.Info("ESI response received",
		"path", path,
		"status", resp.StatusCode,
		"tokens_consumed", tokensConsumed,
		"duration", requestDuration,
		"group", gl.Name,
		"content_length", resp.ContentLength)

	// Handle server feedback synchronously to ensure proper tracking
	// This will discover/create the group limiter if needed
	actualLimiter := c.updateFromHeaders(gl, resp, tokensConsumed, path)

	// If we discovered a new group and were using default, update gl for next iteration
	if actualLimiter != gl {
		logs.Info("switched to discovered group limiter",
			"path", path,
			"old_group", gl.Name,
			"new_group", actualLimiter.Name,
			"new_token_limit", actualLimiter.tokenLimit)
		gl = actualLimiter
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		resp.Body.Close()
		return nil, resp, err
	}

	// Handle 429 responses - return classified error for task to handle
	if resp.StatusCode == http.StatusTooManyRequests {
		resp.Body.Close()
		retryAfterStr := resp.Header.Get("Retry-After")
		retryAfterTime := time.Now().Add(15 * time.Minute) // Default to window duration
		retryable := false

		if retryAfterStr != "" {
			if seconds, err := strconv.Atoi(retryAfterStr); err == nil {
				retryAfterTime = time.Now().Add(time.Duration(seconds) * time.Second)
				retryable = true
			}
		}

		actualLimiter.mu.RLock()
		tokenUsed := actualLimiter.tokenUsed
		tokenLimit := actualLimiter.tokenLimit
		actualLimiter.mu.RUnlock()

		err := &RateLimitError{
			Retryable:       retryable,
			RetryAfter:      retryAfterTime,
			Reason:          "server returned 429 Too Many Requests",
			Group:           actualLimiter.Name,
			TokenUsed:       tokenUsed,
			TokenLimit:      tokenLimit,
			EstimatedTokens: tokensConsumed,
		}

		logs.Warn("received 429, returning classified error for task handling",
			"path", path,
			"group", actualLimiter.Name,
			"retryable", retryable,
			"retry_after", retryAfterTime,
			"retry_after_seconds", retryAfterStr)

		return nil, resp, err
	}

	// Get final token status for logging
	actualLimiter.mu.RLock()
	finalTokenUsed := actualLimiter.tokenUsed
	finalTokenLimit := actualLimiter.tokenLimit
	finalRemaining := finalTokenLimit - finalTokenUsed
	actualLimiter.mu.RUnlock()

	logs.Info("ESI request completed successfully",
		"path", path,
		"group", actualLimiter.Name,
		"status", resp.StatusCode,
		"tokens_consumed", tokensConsumed,
		"token_used", finalTokenUsed,
		"token_limit", finalTokenLimit,
		"token_remaining", finalRemaining,
		"remaining_percent", fmt.Sprintf("%.1f%%", float64(finalRemaining)/float64(finalTokenLimit)*100),
		"rate_limit", fmt.Sprintf("%.2f req/s", actualLimiter.Limiter.Limit()),
		"body_size", len(body))

	return body, resp, nil
}

// DoRequest performs a rate-limited HTTP request and returns just the response.
// This is useful when you need full control over reading the response body (e.g., for streaming).
// Note: Token consumption will be tracked when the response status code is read.
func (c *ESIClient) DoRequest(ctx context.Context, method, path string, headers map[string]string) (*http.Response, error) {
	logs.Debug("ESI request initiated (streaming)",
		"method", method,
		"path", path)

	// Get limiter for this path (or default if unknown)
	gl, known := c.getLimiterForPath(path)

	// If group is unknown, use mutex to prevent concurrent requests to same unknown path
	var pathMutex *sync.Mutex
	if !known {
		logs.Info("acquiring mutex for unknown path discovery (streaming)",
			"path", path,
			"group", gl.Name)
		pathMutex = c.getMutexForUnknownPath(path)
		pathMutex.Lock()
		defer func() {
			pathMutex.Unlock()
			logs.Debug("released mutex for path (streaming)",
				"path", path)
		}()
	}

	// Estimate tokens needed (assume 2XX response = 2 tokens for now)
	estimatedTokens := 2
	logs.Debug("estimated token cost (streaming)",
		"path", path,
		"group", gl.Name,
		"estimated_tokens", estimatedTokens)

	// TEST MODE: Inject rate limit errors before making actual request
	if c.testMode && c.shouldInjectTestError(path) {
		testErr := c.injectTestRateLimitError(gl, estimatedTokens)
		if testErr != nil {
			logs.Warn("TEST MODE: Injecting rate limit error",
				"path", path,
				"group", gl.Name,
				"scenario", c.testModeScenario,
				"error", testErr)
			return nil, testErr
		}
	}

	// Check if we can make the request (token bucket + retry-after)
	// Return immediately with classified error if rate limited - let task decide how to handle
	if err := gl.canMakeRequest(ctx, estimatedTokens); err != nil {
		logs.Debug("rate limit check failed, returning classified error (streaming)",
			"path", path,
			"group", gl.Name,
			"error", err)
		return nil, err
	}

	// Wait for general rate limiter (prevents burst)
	rateWaitStart := time.Now()
	if err := gl.Limiter.Wait(ctx); err != nil {
		logs.Error("rate limiter wait failed (streaming)",
			"path", path,
			"group", gl.Name,
			"error", err)
		return nil, fmt.Errorf("rate wait failed: %w", err)
	}
	rateWaitDuration := time.Since(rateWaitStart)
	if rateWaitDuration > 100*time.Millisecond {
		logs.Debug("rate limiter wait duration (streaming)",
			"path", path,
			"group", gl.Name,
			"		wait_duration", rateWaitDuration)
	}

	logs.Debug("making HTTP request (streaming)",
		"method", method,
		"path", path,
		"group", gl.Name,
		"url", c.baseURL+path)

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}

	// Apply default headers
	httpclient.ApplyDefaultHeaders(req)

	// Apply custom headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	requestStart := time.Now()
	resp, err := c.httpClient.Do(req)
	requestDuration := time.Since(requestStart)
	if err != nil {
		logs.Error("HTTP request failed (streaming)",
			"path", path,
			"group", gl.Name,
			"error", err,
			"duration", requestDuration)
		return nil, err
	}

	// Calculate actual tokens consumed based on status
	tokensConsumed := getTokensForStatus(resp.StatusCode)
	logs.Info("ESI response received (streaming)",
		"path", path,
		"status", resp.StatusCode,
		"tokens_consumed", tokensConsumed,
		"duration", requestDuration,
		"group", gl.Name,
		"content_length", resp.ContentLength)

	// Handle server feedback synchronously to ensure proper tracking
	// This will discover/create the group limiter if needed
	actualLimiter := c.updateFromHeaders(gl, resp, tokensConsumed, path)

	// If we discovered a new group and were using default, update gl for next iteration
	if actualLimiter != gl {
		logs.Info("switched to discovered group limiter (streaming)",
			"path", path,
			"old_group", gl.Name,
			"new_group", actualLimiter.Name,
			"new_token_limit", actualLimiter.tokenLimit)
		gl = actualLimiter
	}

	// Handle 429 responses - return classified error for task to handle
	if resp.StatusCode == http.StatusTooManyRequests {
		resp.Body.Close()
		retryAfterStr := resp.Header.Get("Retry-After")
		retryAfterTime := time.Now().Add(15 * time.Minute) // Default to window duration
		retryable := false

		if retryAfterStr != "" {
			if seconds, err := strconv.Atoi(retryAfterStr); err == nil {
				retryAfterTime = time.Now().Add(time.Duration(seconds) * time.Second)
				retryable = true
			}
		}

		actualLimiter.mu.RLock()
		tokenUsed := actualLimiter.tokenUsed
		tokenLimit := actualLimiter.tokenLimit
		actualLimiter.mu.RUnlock()

		err := &RateLimitError{
			Retryable:       retryable,
			RetryAfter:      retryAfterTime,
			Reason:          "server returned 429 Too Many Requests",
			Group:           actualLimiter.Name,
			TokenUsed:       tokenUsed,
			TokenLimit:      tokenLimit,
			EstimatedTokens: tokensConsumed,
		}

		logs.Warn("received 429, returning classified error for task handling (streaming)",
			"path", path,
			"group", actualLimiter.Name,
			"retryable", retryable,
			"retry_after", retryAfterTime,
			"retry_after_seconds", retryAfterStr)

		return nil, err
	}

	// Get final token status for logging
	actualLimiter.mu.RLock()
	finalTokenUsed := actualLimiter.tokenUsed
	finalTokenLimit := actualLimiter.tokenLimit
	finalRemaining := finalTokenLimit - finalTokenUsed
	actualLimiter.mu.RUnlock()

	logs.Info("ESI request completed (streaming response)",
		"path", path,
		"group", actualLimiter.Name,
		"status", resp.StatusCode,
		"tokens_consumed", tokensConsumed,
		"token_used", finalTokenUsed,
		"token_limit", finalTokenLimit,
		"token_remaining", finalRemaining,
		"remaining_percent", fmt.Sprintf("%.1f%%", float64(finalRemaining)/float64(finalTokenLimit)*100),
		"rate_limit", fmt.Sprintf("%.2f req/s", actualLimiter.Limiter.Limit()))

	return resp, nil
}

// shouldInjectTestError checks if test mode should inject an error for this path
func (c *ESIClient) shouldInjectTestError(path string) bool {
	if !c.testMode {
		return false
	}
	// If no specific path is configured, match all paths
	if c.testModePath == "" {
		return true
	}
	// Otherwise, check if path matches (simple prefix match)
	return strings.HasPrefix(path, c.testModePath)
}

// injectTestRateLimitError creates a test rate limit error based on the configured scenario
func (c *ESIClient) injectTestRateLimitError(gl *GroupLimiter, estimatedTokens int) *RateLimitError {
	gl.mu.RLock()
	tokenUsed := gl.tokenUsed
	tokenLimit := gl.tokenLimit
	gl.mu.RUnlock()

	retryAfter := time.Now().Add(c.testModeRetryAfter)
	retryable := false
	reason := "test mode: " + c.testModeScenario

	switch c.testModeScenario {
	case "retryable_short":
		retryable = true
		retryAfter = time.Now().Add(30 * time.Second) // Short retry
		reason = "test mode: retryable rate limit (short delay)"

	case "retryable_long":
		retryable = true
		retryAfter = time.Now().Add(c.testModeRetryAfter) // Use configured duration
		reason = "test mode: retryable rate limit (long delay)"

	case "non_retryable":
		retryable = false
		retryAfter = time.Time{} // No retry
		reason = "test mode: non-retryable rate limit (insufficient tokens, >5 min wait)"

	case "429_response":
		// This scenario is handled differently - we'll let it make the request and return 429
		// For now, return a 429-like error
		retryable = true
		retryAfter = time.Now().Add(1 * time.Minute)
		reason = "test mode: simulated 429 response (will be handled in HTTP response)"
		// Note: For true 429 testing, we'd need to mock the HTTP response
		return nil // Don't inject here, let it make the request

	default:
		// Unknown scenario, default to retryable short
		retryable = true
		retryAfter = time.Now().Add(30 * time.Second)
		reason = "test mode: default retryable rate limit"
	}

	return &RateLimitError{
		Retryable:       retryable,
		RetryAfter:      retryAfter,
		Reason:          reason,
		Group:           gl.Name,
		TokenUsed:       tokenUsed,
		TokenLimit:      tokenLimit,
		EstimatedTokens: estimatedTokens,
	}
}

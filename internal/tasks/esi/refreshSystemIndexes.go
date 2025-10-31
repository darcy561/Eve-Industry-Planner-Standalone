package esi

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	natscore "eve-industry-planner/internal/core/nats"
	rediscore "eve-industry-planner/internal/core/redis"
	schedulercore "eve-industry-planner/internal/scheduler"
	"eve-industry-planner/internal/shared/logs"
	"eve-industry-planner/internal/shared/metrics"
	taskscore "eve-industry-planner/internal/tasks"

	natslib "github.com/nats-io/nats.go"
	redislib "github.com/redis/go-redis/v9"
)

// ESICostIndice represents an individual cost index returned by ESI.
type ESICostIndice struct {
	Activity  string  `json:"activity"`
	CostIndex float64 `json:"cost_index"`
}

// ESIIndustrySystem mirrors each item in the ESI industry systems response.
type ESIIndustrySystem struct {
	CostIndices   []ESICostIndice `json:"cost_indices"`
	SolarSystemID int32           `json:"solar_system_id"`
}

// SystemIndexes is the normalized structure used internally.
type SystemIndexes struct {
	SolarSystemID    int32   `json:"solar_system_id"`
	LastUpdated      int64   `json:"lastUpdated"`
	Manufacturing    float64 `json:"manufacturing,omitempty"`
	ResearchTime     float64 `json:"researching_time_efficiency,omitempty"`
	ResearchMaterial float64 `json:"researching_material_efficiency,omitempty"`
	Copying          float64 `json:"copying,omitempty"`
	Invention        float64 `json:"invention,omitempty"`
	Reaction         float64 `json:"reaction,omitempty"`
}

// RefreshSystemIndexes fetches the latest industry system cost indices from ESI using a streaming decoder.
// It checks for HTTP 304 Not Modified responses to avoid unnecessary work when data hasn't changed.
// When data has changed, each item is persisted to Redis in the stream callback, and the ETag
// is saved after a successful pass. Cache headers are respected for scheduling future refreshes.
func RefreshSystemIndexes(natsMessage MessageInterface, redisClient *redislib.Client, natsConn *natslib.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client := sharedHTTPClient

	// Acquire a lock to prevent concurrent refreshes
	lockKey := "esi:industry_systems:refresh_lock"
	lockAcquired, cleanup, err := rediscore.AcquireRefreshLock(ctx, redisClient, lockKey)
	if err != nil {
		logs.Warn("failed to acquire refresh lock", "error", err)
		if natsMessage != nil {
			_ = natsMessage.Ack()
		}
		return
	}
	if !lockAcquired {
		logs.Info("skipping refresh, another refresh in progress")
		if natsMessage != nil {
			_ = natsMessage.Ack()
		}
		return
	}
	defer cleanup()

	// Check if we should skip refresh based on next_refresh timestamp
	nextRefresh, err := rediscore.GetIndustrySystemsNextRefresh(ctx, redisClient)
	if err == nil && nextRefresh > 0 {
		now := time.Now().UnixMilli()
		if now < nextRefresh {
			skipDuration := time.Duration(nextRefresh-now) * time.Millisecond
			logs.Info("skipping refresh, not due yet", "next_refresh_ms", nextRefresh, "now_ms", now, "skip_duration_ms", skipDuration.Milliseconds())
			if natsMessage != nil {
				_ = natsMessage.Ack()
			}
			// No need to reschedule - scheduler already has this time scheduled
			return
		}
	}

	// Read previous ETag from Redis (if available) to leverage 304s.
	prevETag, err := rediscore.GetIndustrySystemsETag(ctx, redisClient)
	if err != nil {
		logs.Warn("failed to get previous ETag", "error", err)
	}

	var count int
	lastProgress := time.Now()
	start := time.Now()
	logs.Info("industry systems refresh started", "etag_used", prevETag)
	// initial heartbeat so long fetches don't time out
	if natsMessage != nil {
		_ = natsMessage.InProgress()
	}

	var totalBytes int64
	var cacheSeconds int
	newETag, notModified, bytesRead, err := StreamIndustrySystems(ctx, client, prevETag, func(s SystemIndexes) error {
		if err := rediscore.SaveIndustrySystemIndex(ctx, redisClient, s.SolarSystemID, s); err != nil {
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
		logs.Error("failed streaming ESI industry systems", "error", err, "reason", "stream_error")
		if natsMessage != nil {
			natscore.NackWithBackoff(natsMessage)
		}
		metrics.GetESIIndustrySystems().Errors.WithLabelValues("stream").Inc()
		return
	}

	if notModified {
		logs.Info("ESI industry systems not modified (ETag match)", "etag_new", newETag)
		if natsMessage != nil {
			_ = natsMessage.Ack()
		}
		m := metrics.GetESIIndustrySystems()
		m.Requests.Observe(time.Since(start).Seconds())
		m.Bytes.Add(float64(totalBytes))
		// Update next_refresh timestamp if cache headers available, but don't schedule - assume schedule already exists
		if cacheSeconds > 0 {
			nextRefreshMillis := time.Now().Add(time.Duration(cacheSeconds) * time.Second).UnixMilli()
			_ = rediscore.SetString(ctx, redisClient, "esi:industry_systems:next_refresh", strconv.FormatInt(nextRefreshMillis, 10), 0)
			metrics.GetESIIndustrySystems().NextRefresh.Set(float64(nextRefreshMillis))
		}
		return
	}

	if err := rediscore.SaveIndustrySystemsETag(ctx, redisClient, newETag); err != nil {
		logs.Error("failed to save ETag", "error", err, "reason", "etag_save_error")
		if natsMessage != nil {
			natscore.NackWithBackoff(natsMessage)
		}
		metrics.GetESIIndustrySystems().Errors.WithLabelValues("etag_save").Inc()
		return
	}

	// Save last updated timestamp
	if err := rediscore.SaveIndustrySystemsLastUpdated(ctx, redisClient, time.Now().UnixMilli()); err != nil {
		logs.Warn("failed to save last updated timestamp", "error", err, "reason", "last_updated_save_error")
		if natsMessage != nil {
			natscore.NackWithBackoff(natsMessage)
		}
		metrics.GetESIIndustrySystems().Errors.WithLabelValues("last_updated_save").Inc()
		return
	}

	// Respect cache headers for scheduling next refresh
	var nextRefreshMillis int64
	if cacheSeconds > 0 {
		nextRefreshMillis = time.Now().Add(time.Duration(cacheSeconds) * time.Second).UnixMilli()
		_ = rediscore.SetString(ctx, redisClient, "esi:industry_systems:next_refresh", strconv.FormatInt(nextRefreshMillis, 10), 0)
		metrics.GetESIIndustrySystems().NextRefresh.Set(float64(nextRefreshMillis))
	}

	// Acknowledge message completion
	if natsMessage != nil {
		_ = natsMessage.Ack()
	}

	// Schedule next refresh based on cache headers
	if natsConn != nil && nextRefreshMillis > 0 {
		_ = schedulercore.PublishScheduleRequest(natsConn, taskscore.TaskTypeRefreshSystemIndexes, nextRefreshMillis, nil)
	}

	duration := time.Since(start)
	m := metrics.GetESIIndustrySystems()
	m.Requests.Observe(duration.Seconds())
	m.Bytes.Add(float64(totalBytes))
	m.Items.Add(float64(count))
	m.LastUpdated.Set(float64(time.Now().UnixMilli()))
	logs.Info("updated ESI industry systems", "count", count, "etag_new", newETag, "bytes_read", totalBytes, "duration_ms", duration.Milliseconds())
}

// StreamIndustrySystems makes an HTTP request to ESI and checks the response status code first.
// For HTTP 304 Not Modified responses, it returns early without streaming.
// For HTTP 200 OK responses, it performs a streaming decode of the ESI array and invokes
// onItem for each normalized SystemIndexes. Callers typically persist within the callback.
// Returns the new ETag, whether it was not modified (HTTP 304), bytes read, and any error.
// cacheSecondsOut will be populated with parsed cache max-age from response headers if available.
func StreamIndustrySystems(ctx context.Context, httpClient *http.Client, etag string, onItem func(SystemIndexes) error, cacheSecondsOut *int) (string, bool, int64, error) {
	if httpClient == nil {
		return "", false, 0, errors.New("http client is nil")
	}
	if onItem == nil {
		return "", false, 0, errors.New("onItem callback is nil")
	}

	baseURL := &url.URL{Scheme: "https", Host: "esi.evetech.net", Path: "/v1/industry/systems/"}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL.String(), nil)
	if err != nil {
		return "", false, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("User-Agent", "EveIndustryPlanner/1.0 (contact: admin@local)")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	// Retry on transient errors with exponential backoff + jitter
	var resp *http.Response
	maxAttempts := 4
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		resp, err = httpClient.Do(req)
		if err == nil {
			break
		}
		if attempt >= maxAttempts {
			return "", false, 0, err
		}
		// Exponential backoff: 500ms, 1s, 2s
		backoff := time.Duration(500*(1<<uint(attempt-1))) * time.Millisecond
		// Jitter: random 0-100ms
		jitter := time.Duration(rand.Intn(100)) * time.Millisecond
		time.Sleep(backoff + jitter)
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
		var item ESIIndustrySystem
		if err := dec.Decode(&item); err != nil {
			return newETag, false, cr.n, err
		}
		s := SystemIndexes{SolarSystemID: item.SolarSystemID, LastUpdated: nowMs}
		for _, ci := range item.CostIndices {
			switch ci.Activity {
			case "manufacturing":
				s.Manufacturing = ci.CostIndex
			case "researching_time_efficiency":
				s.ResearchTime = ci.CostIndex
			case "researching_material_efficiency":
				s.ResearchMaterial = ci.CostIndex
			case "copying":
				s.Copying = ci.CostIndex
			case "invention":
				s.Invention = ci.CostIndex
			case "reaction":
				s.Reaction = ci.CostIndex
			}
		}
		if err := onItem(s); err != nil {
			return newETag, false, cr.n, err
		}
	}
	// Consume end of array
	if _, err := dec.Token(); err != nil {
		return newETag, false, cr.n, err
	}

	return newETag, false, cr.n, nil
}

// saveSystemIndexes stores each system's data in Redis as JSON at key "esi:industry_systems:<solarSystemId>".
func saveSystemIndexes(ctx context.Context, rdb *redislib.Client, systems []SystemIndexes) error {
	for _, s := range systems {
		if err := rediscore.SaveIndustrySystemIndex(ctx, rdb, s.SolarSystemID, s); err != nil {
			return err
		}
	}
	return nil
}

// countingReader counts bytes read from an underlying reader
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// parseCacheSeconds extracts max-age seconds from Cache-Control or computes from Expires header
func parseCacheSeconds(resp *http.Response) int {
	cc := resp.Header.Get("Cache-Control")
	if cc != "" {
		parts := strings.Split(cc, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if strings.HasPrefix(p, "max-age=") {
				v := strings.TrimPrefix(p, "max-age=")
				if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
					return secs
				}
			}
		}
	}
	if exp := resp.Header.Get("Expires"); exp != "" {
		if t, err := http.ParseTime(exp); err == nil {
			d := time.Until(t)
			if d > 0 {
				return int(d.Seconds())
			}
		}
	}
	return 0
}

// Shared HTTP client with tuned transport
var sharedHTTPClient = &http.Client{
	Transport: tunedTransport(),
	Timeout:   30 * time.Second,
}

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

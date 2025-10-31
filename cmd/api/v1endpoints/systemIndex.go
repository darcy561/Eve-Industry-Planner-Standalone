package v1endpoints

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	rediscore "eve-industry-planner/internal/core/redis"
	"eve-industry-planner/internal/shared/logs"
	"eve-industry-planner/internal/shared/metrics"
	"eve-industry-planner/internal/tasks/esi"

	"github.com/redis/go-redis/v9"
)

const (
	maxSystemIDs = 500
	validIDRegex = `^\d+$`
)

var validIDPattern = regexp.MustCompile(validIDRegex)

// SystemIndexesHandler handles GET and POST requests for system indexes
// GET: expects system ID in query parameter ?id=12345
// POST: expects array of system IDs in body ["12345", "67890"]
func SystemIndexesHandler(w http.ResponseWriter, r *http.Request, redisClient *redis.Client) {
	start := time.Now()
	m := metrics.GetAPISystemIndexes()

	// Set context timeout
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	w.Header().Set("Content-Type", "application/json")

	var systemIDs []string
	var err error

	switch r.Method {
	case http.MethodGet:
		systemIDs, err = extractSystemIDsFromGet(r)
	case http.MethodPost:
		systemIDs, err = extractSystemIDsFromPost(r)
	default:
		m.Errors.WithLabelValues("method_not_allowed").Inc()
		logs.Warn("invalid method for system indexes endpoint", "method", r.Method, "path", r.URL.Path, "ip", r.RemoteAddr)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err != nil {
		m.Errors.WithLabelValues("extraction_error").Inc()
		logs.Warn("failed to extract system IDs", "error", err, "method", r.Method, "ip", r.RemoteAddr)
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Validate and clean system IDs
	validatedIDs, invalidCount := validateSystemIDs(systemIDs)
	if len(validatedIDs) == 0 {
		m.Errors.WithLabelValues("no_valid_ids").Inc()
		logs.Warn("no valid system IDs provided", "total_ids", len(systemIDs), "invalid_ids", invalidCount, "ip", r.RemoteAddr)
		http.Error(w, "No valid system IDs provided", http.StatusBadRequest)
		return
	}

	if len(validatedIDs) > maxSystemIDs {
		m.Errors.WithLabelValues("too_many_ids").Inc()
		logs.Warn("too many system IDs requested", "count", len(validatedIDs), "max", maxSystemIDs, "ip", r.RemoteAddr)
		http.Error(w, fmt.Sprintf("Too many system IDs (max %d)", maxSystemIDs), http.StatusBadRequest)
		return
	}

	if invalidCount > 0 {
		logs.Info("some invalid system IDs filtered out", "total_ids", len(systemIDs), "valid_ids", len(validatedIDs), "invalid_ids", invalidCount, "ip", r.RemoteAddr)
	}

	// Retrieve system indexes from Redis
	result := make(map[string]esi.SystemIndexes, len(validatedIDs))
	systemsFound := 0
	systemsNotFound := 0

	for _, idStr := range validatedIDs {
		systemID, _ := strconv.ParseInt(idStr, 10, 32) // Already validated, so no error check needed

		var index esi.SystemIndexes
		err = rediscore.GetIndustrySystemIndex(ctx, redisClient, int32(systemID), &index)
		if err != nil {
			// System not found in Redis - return blank index
			systemsNotFound++
			m.SystemsNotFoundByID.WithLabelValues(idStr).Inc()
			index = esi.SystemIndexes{
				SolarSystemID:    int32(systemID),
				LastUpdated:      0,
				Manufacturing:    0,
				ResearchTime:     0,
				ResearchMaterial: 0,
				Copying:          0,
				Invention:        0,
				Reaction:         0,
			}

			// Log error if it's not just a missing entry
			if err != redis.Nil {
				m.Errors.WithLabelValues("redis_error").Inc()
				logs.Warn("redis error retrieving system index", "error", err, "system_id", systemID, "ip", r.RemoteAddr)
			}
		} else {
			systemsFound++
		}
		result[idStr] = index
	}

	// Update metrics
	duration := time.Since(start)
	m.Requests.Observe(duration.Seconds())
	m.RequestsCount.Inc()
	m.SystemsRequested.Observe(float64(len(validatedIDs)))
	m.SystemsFound.Add(float64(systemsFound))
	m.SystemsNotFound.Add(float64(systemsNotFound))

	// Log successful request
	logs.Info("system indexes request completed",
		"method", r.Method,
		"system_ids_count", len(validatedIDs),
		"systems_found", systemsFound,
		"systems_not_found", systemsNotFound,
		"duration_ms", duration.Milliseconds(),
		"ip", r.RemoteAddr,
	)

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(result); err != nil {
		m.Errors.WithLabelValues("encode_error").Inc()
		logs.Error("failed to encode response", "error", err, "ip", r.RemoteAddr)
	}
}

func extractSystemIDsFromGet(r *http.Request) ([]string, error) {
	ids := r.URL.Query()["id"]
	if len(ids) == 0 {
		return nil, fmt.Errorf("missing 'id' query parameter")
	}
	return ids, nil
}

func extractSystemIDsFromPost(r *http.Request) ([]string, error) {
	var ids []string
	if err := json.NewDecoder(r.Body).Decode(&ids); err != nil {
		return nil, fmt.Errorf("invalid JSON body: %w", err)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("empty array in request body")
	}
	return ids, nil
}

// validateSystemIDs validates and filters system IDs, returning valid IDs and count of invalid ones
func validateSystemIDs(ids []string) ([]string, int) {
	validated := make([]string, 0, len(ids))
	invalidCount := 0

	for _, idStr := range ids {
		// Check format: must be numeric only
		if !validIDPattern.MatchString(idStr) {
			invalidCount++
			continue
		}

		// Check if it can be parsed as int32
		if _, err := strconv.ParseInt(idStr, 10, 32); err != nil {
			invalidCount++
			continue
		}

		validated = append(validated, idStr)
	}

	return validated, invalidCount
}

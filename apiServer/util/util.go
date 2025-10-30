package util

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"time"

	"eveindustryplanner.com/esiworkers/shared/contextkeys"
)

type Response struct {
	Message string `json:"message,omitempty"`
	Token   string `json:"token,omitempty"`
	Data    any    `json:"data,omitempty"`
}

const (
	maxIDs = 500
)

func JsonResponse(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Encoding", "gzip")
	w.WriteHeader(http.StatusOK)

	gz := gzip.NewWriter(w)
	defer gz.Close()

	if err := json.NewEncoder(gz).Encode(data); err != nil {
		http.Error(w, fmt.Sprintf("Error encoding response: %v", err), http.StatusInternalServerError)
	}
}

func PopulateStruct(data map[string]string, out any) error {
	outVal := reflect.ValueOf(out).Elem()
	outType := outVal.Type()

	for i := 0; i < outType.NumField(); i++ {
		field := outType.Field(i)
		fieldVal := outVal.Field(i)

		redisValue, ok := data[field.Name]
		if !ok {
			continue
		}

		switch fieldVal.Kind() {
		case reflect.String:
			fieldVal.SetString(redisValue)
		case reflect.Int, reflect.Int64:
			intVal, err := strconv.ParseInt(redisValue, 10, 64)
			if err == nil {
				fieldVal.SetInt(intVal)
			}
		case reflect.Float64, reflect.Float32:
			floatVal, err := strconv.ParseFloat(redisValue, 64)
			if err == nil {
				fieldVal.SetFloat(floatVal)
			}
		case reflect.Bool:
			boolVal, err := strconv.ParseBool(redisValue)
			if err == nil {
				fieldVal.SetBool(boolVal)
			}
		}
	}
	return nil

}

func LogRequestCompletion(r *http.Request, log *slog.Logger) {
	var duration = "N/A"
	if val, ok := r.Context().Value(contextkeys.RequestTimeKey).(time.Time); ok {
		duration = time.Since(val).String()
	}

	log.Info("Request completed",
		slog.Int("status_code", http.StatusOK),
		slog.String("duration", duration),
	)
}

func ExtractIDs(r *http.Request) ([]string, error) {
	var ids []string
	var err error
	switch r.Method {
	case http.MethodGet:
		ids, err = extractIDsFromGet(r)
	case http.MethodPost:
		ids, err = extractIDsFromPost(r)
	default:
		err = fmt.Errorf("method not allowed")
	}
	return ids, err
}

func extractIDsFromGet(r *http.Request) ([]string, error) {
	ids := r.URL.Query()["id"]
	if len(ids) == 0 {
		return nil, fmt.Errorf("missing ID parameter")
	}

	if len(ids) > 500 {
		return nil, fmt.Errorf("too many IDs (max 500)")
	}

	validID := regexp.MustCompile(`^\d+$`)
	for _, id := range ids {
		if !validID.MatchString(id) {
			return nil, fmt.Errorf("invalid ID format")
		}
	}

	ids = removeDuplicateIDS(ids)
	return ids, nil
}

func extractIDsFromPost(r *http.Request) ([]string, error) {
	var ids []string
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&ids); err != nil {
		return nil, fmt.Errorf("invalid request body")
	}

	if len(ids) == 0 {
		return nil, fmt.Errorf("missing ID parameter")
	}

	if len(ids) > maxIDs {
		return nil, fmt.Errorf("too many IDs (max 500)")
	}

	validID := regexp.MustCompile(`^\d+$`)
	for _, id := range ids {
		if !validID.MatchString(id) {
			return nil, fmt.Errorf("invalid ID format")
		}
	}
	return ids, nil
}

func removeDuplicateIDS(slice []string) []string {
	seen := make(map[string]bool)
	result := []string{}

	for _, str := range slice {
		if !seen[str] {
			seen[str] = true
			result = append(result, str)
		}
	}

	return result
}

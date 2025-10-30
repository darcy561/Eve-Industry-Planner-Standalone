package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"eveindustryplanner.com/esiworkers/shared/contextkeys"
	"github.com/google/uuid"
)

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		start := time.Now()

		traceID := r.Header.Get("X-Trace-ID")
		if traceID == "" {
			traceID = uuid.New().String()
		}
		var ids []string
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			ids = captureRequestBody(r)
		} else if r.Method == http.MethodGet {
			ids = captureGetQueryParams(r)
		}

		idsJson, _ := json.Marshal(ids)

		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		ctx := context.WithValue(r.Context(), contextkeys.TraceIDKey, traceID)
		ctx = context.WithValue(ctx, contextkeys.MethodKey, r.Method)
		ctx = context.WithValue(ctx, contextkeys.PathKey, r.URL.Path)
		ctx = context.WithValue(ctx, contextkeys.RemoteIPKey, r.RemoteAddr)
		ctx = context.WithValue(ctx, contextkeys.StatusCodeKey, rw.statusCode)
		ctx = context.WithValue(ctx, contextkeys.RequestTimeKey, start)
		ctx = context.WithValue(ctx, contextkeys.RequestData, string(idsJson))

		next.ServeHTTP(rw, r.WithContext(ctx))

	})
}

func captureRequestBody(r *http.Request) []string {
	if r.Body == nil {
		return []string{}
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return []string{}
	}

	r.Body = io.NopCloser(bytes.NewReader(body))

	var ids []string
	if err := json.Unmarshal(body, &ids); err == nil {
		return ids
	}

	return []string{}
}

func captureGetQueryParams(r *http.Request) []string {
	query := r.URL.Query()
	ids := query["id"]

	if len(ids) == 0 {
		return []string{}
	}

	return ids
}

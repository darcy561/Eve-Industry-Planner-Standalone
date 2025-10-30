package middleware

import (
	"net/http"

	"eve-industry-planner/internal/shared/logs"

	"github.com/ulule/limiter/v3"
	lstdlib "github.com/ulule/limiter/v3/drivers/middleware/stdlib"
)

func RateLimiterConstructor(store limiter.Store, rateLimit limiter.Rate) MiddlewareConstructor {
	return func(next http.Handler) http.Handler {
		l := limiter.New(store, rateLimit, limiter.WithTrustForwardHeader(true))
		mw := lstdlib.NewMiddleware(l)
		inner := mw.Handler(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sr := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			inner.ServeHTTP(sr, r)
			clientIP := r.RemoteAddr
			remaining := sr.Header().Get("X-RateLimit-Remaining")
			if sr.status == http.StatusTooManyRequests {
				logs.Warn("request rate-limited", "method", r.Method, "path", r.URL.Path, "ip", clientIP, "remaining", remaining)
				return
			}
			logs.Info("request allowed", "method", r.Method, "path", r.URL.Path, "ip", clientIP, "remaining", remaining)
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

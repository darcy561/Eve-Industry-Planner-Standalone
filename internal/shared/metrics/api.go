package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// APISystemIndexesMetrics holds metrics for the system indexes API endpoint
type APISystemIndexesMetrics struct {
	Requests            prometheus.Histogram
	RequestsCount       prometheus.Counter
	SystemsRequested    prometheus.Histogram
	SystemsFound        prometheus.Counter
	SystemsNotFound     prometheus.Counter
	SystemsNotFoundByID *prometheus.CounterVec
	Errors              *prometheus.CounterVec
}

var apiSystemIndexesMetrics *APISystemIndexesMetrics

// InitAPISystemIndexes initializes and registers metrics for the system indexes API
func InitAPISystemIndexes() *APISystemIndexesMetrics {
	if apiSystemIndexesMetrics != nil {
		return apiSystemIndexesMetrics
	}

	apiSystemIndexesMetrics = &APISystemIndexesMetrics{
		Requests: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "eip",
			Subsystem: "api",
			Name:      "system_indexes_request_seconds",
			Help:      "Duration of system indexes API requests in seconds",
			Buckets:   prometheus.DefBuckets,
		}),
		RequestsCount: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "eip",
			Subsystem: "api",
			Name:      "system_indexes_requests_total",
			Help:      "Total number of system indexes API requests",
		}),
		SystemsRequested: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "eip",
			Subsystem: "api",
			Name:      "system_indexes_systems_requested",
			Help:      "Number of system IDs requested per API call",
			Buckets:   []float64{1, 5, 10, 25, 50, 100, 250, 500},
		}),
		SystemsFound: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "eip",
			Subsystem: "api",
			Name:      "system_indexes_found_total",
			Help:      "Total number of system indexes found in Redis",
		}),
		SystemsNotFound: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "eip",
			Subsystem: "api",
			Name:      "system_indexes_not_found_total",
			Help:      "Total number of system indexes not found in Redis",
		}),
		SystemsNotFoundByID: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "eip",
				Subsystem: "api",
				Name:      "system_indexes_not_found_by_id_total",
				Help:      "Total number of times a specific system ID was not found in Redis",
			},
			[]string{"system_id"},
		),
		Errors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "eip",
				Subsystem: "api",
				Name:      "system_indexes_errors_total",
				Help:      "Total errors by category",
			},
			[]string{"category"},
		),
	}

	prometheus.MustRegister(
		apiSystemIndexesMetrics.Requests,
		apiSystemIndexesMetrics.RequestsCount,
		apiSystemIndexesMetrics.SystemsRequested,
		apiSystemIndexesMetrics.SystemsFound,
		apiSystemIndexesMetrics.SystemsNotFound,
		apiSystemIndexesMetrics.SystemsNotFoundByID,
		apiSystemIndexesMetrics.Errors,
	)

	return apiSystemIndexesMetrics
}

// GetAPISystemIndexes returns the API system indexes metrics, initializing if needed
func GetAPISystemIndexes() *APISystemIndexesMetrics {
	if apiSystemIndexesMetrics == nil {
		return InitAPISystemIndexes()
	}
	return apiSystemIndexesMetrics
}

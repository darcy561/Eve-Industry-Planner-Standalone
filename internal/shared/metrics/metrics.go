package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// ESIIndustrySystemsMetrics holds all metrics for ESI industry systems operations
type ESIIndustrySystemsMetrics struct {
	Requests    prometheus.Histogram
	Bytes       prometheus.Counter
	Items       prometheus.Counter
	LastUpdated prometheus.Gauge
	NextRefresh prometheus.Gauge
	Errors      *prometheus.CounterVec
}

var industrySystemsMetrics *ESIIndustrySystemsMetrics

// InitESIIndustrySystems initializes and registers metrics for ESI industry systems
func InitESIIndustrySystems() *ESIIndustrySystemsMetrics {
	if industrySystemsMetrics != nil {
		return industrySystemsMetrics
	}

	industrySystemsMetrics = &ESIIndustrySystemsMetrics{
		Requests: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "eip",
			Subsystem: "esi",
			Name:      "industry_systems_request_seconds",
			Help:      "Duration of industry systems requests in seconds",
		}),
		Bytes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "eip",
			Subsystem: "esi",
			Name:      "industry_systems_bytes_total",
			Help:      "Total bytes read from ESI industry systems endpoint",
		}),
		Items: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "eip",
			Subsystem: "esi",
			Name:      "industry_systems_items_total",
			Help:      "Total items processed from ESI industry systems",
		}),
		LastUpdated: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "eip",
			Subsystem: "esi",
			Name:      "industry_systems_last_updated",
			Help:      "Last updated timestamp in milliseconds",
		}),
		NextRefresh: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "eip",
			Subsystem: "esi",
			Name:      "industry_systems_next_refresh",
			Help:      "Next refresh timestamp in milliseconds based on cache headers",
		}),
		Errors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "eip",
				Subsystem: "esi",
				Name:      "industry_systems_errors_total",
				Help:      "Total errors by category",
			},
			[]string{"category"},
		),
	}

	prometheus.MustRegister(
		industrySystemsMetrics.Requests,
		industrySystemsMetrics.Bytes,
		industrySystemsMetrics.Items,
		industrySystemsMetrics.LastUpdated,
		industrySystemsMetrics.NextRefresh,
		industrySystemsMetrics.Errors,
	)

	return industrySystemsMetrics
}

// GetESIIndustrySystems returns the ESI industry systems metrics, initializing if needed
func GetESIIndustrySystems() *ESIIndustrySystemsMetrics {
	if industrySystemsMetrics == nil {
		return InitESIIndustrySystems()
	}
	return industrySystemsMetrics
}

// ESIMarketPricesMetrics holds all metrics for ESI market prices operations
type ESIMarketPricesMetrics struct {
	Requests    prometheus.Histogram
	Bytes       prometheus.Counter
	Items       prometheus.Counter
	LastUpdated prometheus.Gauge
	NextRefresh prometheus.Gauge
	Errors      *prometheus.CounterVec
}

var marketPricesMetrics *ESIMarketPricesMetrics

// InitESIMarketPrices initializes and registers metrics for ESI market prices
func InitESIMarketPrices() *ESIMarketPricesMetrics {
	if marketPricesMetrics != nil {
		return marketPricesMetrics
	}

	marketPricesMetrics = &ESIMarketPricesMetrics{
		Requests: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "eip",
			Subsystem: "esi",
			Name:      "market_prices_request_seconds",
			Help:      "Duration of market prices requests in seconds",
		}),
		Bytes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "eip",
			Subsystem: "esi",
			Name:      "market_prices_bytes_total",
			Help:      "Total bytes read from ESI market prices endpoint",
		}),
		Items: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "eip",
			Subsystem: "esi",
			Name:      "market_prices_items_total",
			Help:      "Total items processed from ESI market prices",
		}),
		LastUpdated: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "eip",
			Subsystem: "esi",
			Name:      "market_prices_last_updated",
			Help:      "Last updated timestamp in milliseconds",
		}),
		NextRefresh: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "eip",
			Subsystem: "esi",
			Name:      "market_prices_next_refresh",
			Help:      "Next refresh timestamp in milliseconds based on cache headers",
		}),
		Errors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "eip",
				Subsystem: "esi",
				Name:      "market_prices_errors_total",
				Help:      "Total errors by category",
			},
			[]string{"category"},
		),
	}

	prometheus.MustRegister(
		marketPricesMetrics.Requests,
		marketPricesMetrics.Bytes,
		marketPricesMetrics.Items,
		marketPricesMetrics.LastUpdated,
		marketPricesMetrics.NextRefresh,
		marketPricesMetrics.Errors,
	)

	return marketPricesMetrics
}

// GetESIMarketPrices returns the ESI market prices metrics, initializing if needed
func GetESIMarketPrices() *ESIMarketPricesMetrics {
	if marketPricesMetrics == nil {
		return InitESIMarketPrices()
	}
	return marketPricesMetrics
}

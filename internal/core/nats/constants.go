package nats

// Stream names
const (
	// StreamESIRefresh is the JetStream stream name for ESI refresh tasks
	StreamESIRefresh = "ESI_REFRESH"
)

// Subject names for task scheduling and processing
const (
	// SubjectRefreshSystemIndexes is the NATS subject for system indexes refresh tasks
	SubjectRefreshSystemIndexes = "refreshSystemIndexes"

	// SubjectRefreshAdjustedPrices is the NATS subject for adjusted prices refresh tasks
	SubjectRefreshAdjustedPrices = "refreshAdjustedPrices"
)

// Consumer names for JetStream pull consumers
const (
	// ConsumerWorkerSystemIndexes is the durable consumer name for system indexes worker
	ConsumerWorkerSystemIndexes = "worker-system-indexes"

	// ConsumerWorkerAdjustedPrices is the durable consumer name for adjusted prices worker
	ConsumerWorkerAdjustedPrices = "worker-adjusted-prices"
)

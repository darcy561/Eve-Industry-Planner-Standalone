package nats

// Stream names
const (
	// StreamESIRefresh is the JetStream stream name for ESI refresh tasks
	StreamESIRefresh = "ESI_REFRESH"

	// StreamScheduler is the JetStream stream name for scheduler task requests
	StreamScheduler = "SCHEDULER"
)

// Subject names for task scheduling and processing
const (
	// SubjectRefreshSystemIndexes is the NATS subject for system indexes refresh tasks
	SubjectRefreshSystemIndexes = "refreshSystemIndexes"

	// SubjectRefreshAdjustedPrices is the NATS subject for adjusted prices refresh tasks
	SubjectRefreshAdjustedPrices = "refreshAdjustedPrices"

	// SubjectSchedulerSchedule is the NATS subject for requesting one-time scheduled tasks
	SubjectSchedulerSchedule = "scheduler:schedule"
)

// Consumer names for JetStream pull consumers
const (
	// ConsumerWorkerSystemIndexes is the durable consumer name for system indexes worker
	ConsumerWorkerSystemIndexes = "worker-system-indexes"

	// ConsumerWorkerAdjustedPrices is the durable consumer name for adjusted prices worker
	ConsumerWorkerAdjustedPrices = "worker-adjusted-prices"

	// ConsumerScheduler is the durable consumer name for scheduler
	ConsumerScheduler = "scheduler"
)

// Task names for logging purposes (human-readable labels)
const (
	// TaskNameSystemIndexesRefresh is the human-readable name for system indexes refresh task
	TaskNameSystemIndexesRefresh = "system indexes refresh"

	// TaskNameAdjustedPricesRefresh is the human-readable name for adjusted prices refresh task
	TaskNameAdjustedPricesRefresh = "adjusted prices refresh"
)

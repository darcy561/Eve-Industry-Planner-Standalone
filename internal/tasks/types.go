package tasks

// Task type identifiers for scheduler registration and NATS messaging
const (
	// TaskTypeRefreshSystemIndexes is the task type identifier for system indexes refresh
	TaskTypeRefreshSystemIndexes = "refreshSystemIndexes"

	// TaskTypeRefreshAdjustedPrices is the task type identifier for adjusted prices refresh
	TaskTypeRefreshAdjustedPrices = "refreshAdjustedPrices"
)

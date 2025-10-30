package scheduler

import (
	"context"
	"log/slog"

	"eveindustryplanner.com/esiworkers/logger"
	"eveindustryplanner.com/esiworkers/scheduler/tasks"
	"github.com/robfig/cron/v3"
)

var instance *cron.Cron

type LoggerWrapper struct {
	logger *slog.Logger
}

func (l LoggerWrapper) Info(msg string, keysAndValues ...any) {
	l.logger.Info(msg, append([]any{"package", logger.Scheduler}, keysAndValues...)...)
}

func (l LoggerWrapper) Error(err error, msg string, keysAndValues ...any) {
	l.logger.Error(msg, append(keysAndValues, "error", err, "package", logger.Scheduler)...)
}

func Start() {
	logger := logger.GetLogger(context.Background(), logger.Scheduler)
	cronLogger := LoggerWrapper{logger: logger}
	instance = cron.New(cron.WithLogger(cronLogger))

	addCronTasks()
	instance.Start()
}

func Stop() {
	if instance != nil {
		instance.Stop()
	}
}

func addCronTasks() {
	instance.AddFunc("0 13 * * *", tasks.RefreshAdjustedPrices)
	instance.AddFunc("@every 10m", tasks.RefreshMarketPrices)
	instance.AddFunc("@every 2h", tasks.RefreshMarketHistory)
	instance.AddFunc("@hourly", tasks.RefreshSystemIndexes)
}

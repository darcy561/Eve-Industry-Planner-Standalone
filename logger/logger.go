package logger

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"sync"

	"eveindustryplanner.com/esiworkers/shared/contextkeys"
)

type loggerModules string

const (
	ApiProvider   loggerModules = "api"
	Scheduler     loggerModules = "schedule"
	Workers       loggerModules = "workers"
	Redis         loggerModules = "redis"
	MessageBroker loggerModules = "messageService"
)

var (
	globalLogger *slog.Logger
	once         sync.Once
)

func InitLogger() {
	once.Do(func() {
		opts := &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}
		handler := slog.NewJSONHandler(os.Stdout, opts)
		globalLogger = slog.New(handler)
	})
}

func GetLogger(ctx context.Context, module loggerModules) *slog.Logger {
	if globalLogger == nil {
		InitLogger()
	}

	logger := globalLogger

	if module == ApiProvider {
		logger = logger.With(
			slog.String("package", string(ApiProvider)),
			slog.String("trace_id", getCtxValue(ctx, contextkeys.TraceIDKey)),
			slog.String("method", getCtxValue(ctx, contextkeys.MethodKey)),
			slog.String("path", getCtxValue(ctx, contextkeys.PathKey)),
			slog.String("remote_ip", getCtxValue(ctx, contextkeys.RemoteIPKey)),
			slog.String("request_data", getCtxValue(ctx, contextkeys.RequestData)),
		)
	}

	if module == Scheduler {
		logger = logger.With(
			slog.String("package", string(Scheduler)),
		)
	}

	if module == Workers {
		logger = logger.With(
			slog.String("package", string(Workers)),
		)
	}

	if module == Redis {
		logger = logger.With(
			slog.String("package", string(Redis)),
		)
	}

	if module == MessageBroker {
		logger = logger.With(
			slog.String("package", string(MessageBroker)),
		)
	}

	return logger
}

func getCtxValue(ctx context.Context, key any) string {
	if val, ok := ctx.Value(key).(string); ok {
		return val
	}
	// Handle status code as int
	if val, ok := ctx.Value(key).(int); ok {
		return strconv.Itoa(val)
	}
	return "N/A"
}

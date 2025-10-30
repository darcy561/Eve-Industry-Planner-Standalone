package ws

import (
	"eve-industry-planner/internal/shared/logs"
)

func StartServer(addr string) error {
	logs.Info("ws server starting", "addr", addr)
	return nil
}

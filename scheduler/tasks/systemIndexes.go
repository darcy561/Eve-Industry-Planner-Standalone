package tasks

import (
	"context"

	"eveindustryplanner.com/esiworkers/logger"
	"eveindustryplanner.com/esiworkers/messagebroker"
)

func RefreshSystemIndexes() {

	log := logger.GetLogger(context.Background(), logger.Scheduler)

	err := messagebroker.PublishMessage(messagebroker.SystemIndexes, []byte(""))
	if err != nil {
		log.Error(err.Error())
	}

	err = messagebroker.Flush()
	if err != nil {
		log.Warn(err.Error())
	}

	log.Info("system index refresh triggered")
}

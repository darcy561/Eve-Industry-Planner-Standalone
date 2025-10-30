package tasks

import (
	"context"

	"eveindustryplanner.com/esiworkers/logger"
	"eveindustryplanner.com/esiworkers/messagebroker"
)

func RefreshAdjustedPrices() {
	log := logger.GetLogger(context.Background(), logger.Scheduler)

	err := messagebroker.PublishMessage(messagebroker.AdjustedPrices, []byte(""))
	if err != nil {
		log.Error("failed to refresh adjusted prices")

	}

	err = messagebroker.Flush()
	if err != nil {
		log.Error(err.Error())
	}

	log.Info("adjusted price refresh triggered")
}

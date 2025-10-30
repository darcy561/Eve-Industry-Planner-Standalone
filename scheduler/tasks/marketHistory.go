package tasks

import (
	"context"
	"fmt"
	"time"

	"eveindustryplanner.com/esiworkers/logger"
	"eveindustryplanner.com/esiworkers/messagebroker"
	"eveindustryplanner.com/esiworkers/redisDB"
)

func RefreshMarketHistory() {
	log := logger.GetLogger(context.Background(), logger.Scheduler)

	redisKey := "marketHistory_lastRefresh"
	results, err := redisDB.RetrieveOldestValuesFromSortedSet(context.Background(), redisKey, 150)
	if err != nil {
		log.Error(err.Error())
		return
	}

	messageCounter := 0
	for _, result := range results {

		if !result.LastUpdated.Before(time.Now().Add(-4 * time.Hour)) {
			continue
		}

		err = messagebroker.PublishMessage(messagebroker.MarketHistory, []byte(result.TypeID))
		if err != nil {
			message := fmt.Sprintf("failed to publish message: %v", result.TypeID)
			log.Warn(message)
			continue
		}

		messageCounter++
		if messageCounter > 100 {
			break
		}

	}

	err = messagebroker.Flush()
	if err != nil {
		log.Warn(err.Error())
	}

	message := fmt.Sprintf("Found %v Market History Entries To Update", messageCounter)
	log.Info(message)
}

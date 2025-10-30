package worker

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"eveindustryplanner.com/esiworkers/esi"
	"eveindustryplanner.com/esiworkers/messagebroker"
	"eveindustryplanner.com/esiworkers/redisDB"
	"github.com/nats-io/nats.go"
)

func SubscribeMarketHistory(wg *sync.WaitGroup) {
	defer wg.Done()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	fmt.Println("Market History Service Worker Conncted & Listening")

	messagebroker.SubscribeToMessages(messagebroker.MarketHistory, handleMarketHistory)

	<-stop

	fmt.Println("Shutting down the Market History Worker...")

	fmt.Println("Market History Worker stopped.")
}

func handleMarketHistory(msg *nats.Msg) {
	ctx := context.Background()
	messageContent, err := strconv.Atoi(string(msg.Data))
	if err != nil {
		log.Printf("failed to convert msg body")
		return
	}

	fmt.Printf("Market History Worker recieved a message: %v\n", messageContent)

	for _, location := range esi.DEFAULT_MARKET_LOCATIONS {
		etagKey := fmt.Sprintf("%v-marketHistory-%v-etags", messageContent, location.ID)
		existingETags, err := redisDB.GetETagsAsMap(ctx, etagKey)

		if err != nil {
			fmt.Printf("error retrieving map from redis: %v", etagKey)
		}

		result, newETags, err := esi.FetchRegionHistory(location, messageContent, existingETags)

		if err != nil {
			fmt.Printf("error: %v", err)
			continue
		}

		refreshKey := "marketHistory_lastRefresh"
		dataKey := fmt.Sprintf("%v-marketHistory-%v", messageContent, location.ID)

		err = redisDB.SaveToSortedSet(ctx, refreshKey, string(msg.Data), float64(time.Now().UnixMilli()))
		if err != nil {
			fmt.Printf("error setting redis value for key %v: %v", refreshKey, err)
			continue
		}
		err = redisDB.SaveStructAsHash(ctx, dataKey, result)
		if err != nil {
			fmt.Printf("error saving market history data to redis for key %v: %v", dataKey, err)
			continue
		}
		err = redisDB.SaveETagsAsHash(ctx, etagKey, newETags)
		if err != nil {
			fmt.Printf("error saving map to redis: %v", etagKey)
			continue
		}

		log.Printf("success")
	}
}

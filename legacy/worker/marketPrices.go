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

const (
	refreshKey = "marketPrices_lastRefresh"
)

func SubscribeMarketPrices(wg *sync.WaitGroup) {
	defer wg.Done()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	fmt.Println("Market Prices Service Worker Conncted & Listening")

	messagebroker.SubscribeToMessages("refreshMarketPrices", handleMarketPrices)

	<-stop

	fmt.Println("Shutting down the Market Prices Worker...")
	fmt.Println("Market Prices Worker stopped.")
}

func handleMarketPrices(msg *nats.Msg) {
	ctx := context.Background()
	messageContent, err := strconv.Atoi(string(msg.Data))
	if err != nil {
		log.Printf("failed to convert msg body")
		return
	}

	fmt.Printf("Market Prices Worker recieved a message: %v\n", messageContent)
	FetchMarketPrices(ctx, messageContent)
}

func FetchMarketPrices(ctx context.Context, id int) map[string]esi.DBMarketPriceEntry {
	idAsString := strconv.Itoa(id)

	results := make(map[string]esi.DBMarketPriceEntry)

	for _, location := range esi.DEFAULT_MARKET_LOCATIONS {
		locationIDAsString := strconv.Itoa(location.ID)

		etagKey := fmt.Sprintf("%v-marketPrices-%v-etags", id, location.ID)
		existingETags, err := redisDB.GetETagsAsMap(ctx, etagKey)

		if err != nil {
			fmt.Printf("error retrieving map from redis: %v", etagKey)
		}

		result, newETags, err := esi.FetchStationPrices(location, id, existingETags)
		if err != nil {
			fmt.Printf("error: %v", err)
			continue
		}

		dataKey := fmt.Sprintf("%v-marketPrices-%v", id, location.ID)

		err = redisDB.SaveToSortedSet(ctx, refreshKey, idAsString, float64(time.Now().UnixMilli()))
		if err != nil {
			fmt.Printf("error setting redis value for key %v: %v", refreshKey, err)
			continue
		}

		err = redisDB.SaveStructAsHash(ctx, dataKey, result)
		if err != nil {
			fmt.Printf("error saving market price data to redis for key %v: %v", dataKey, err)
			continue
		}

		err = redisDB.SaveETagsAsHash(ctx, etagKey, newETags)
		if err != nil {
			fmt.Printf("error saving map to redis: %v", etagKey)
		}

		results[locationIDAsString] = result

		log.Printf("success")

	}
	return results
}

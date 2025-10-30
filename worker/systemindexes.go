package worker

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"eveindustryplanner.com/esiworkers/esi"
	"eveindustryplanner.com/esiworkers/messagebroker"
	"eveindustryplanner.com/esiworkers/redisDB"
	"github.com/nats-io/nats.go"
)

func SubscribeSystemIndexes(wg *sync.WaitGroup) {
	defer wg.Done()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	fmt.Println("System Index Service Worker Conncted & Listening")

	messagebroker.SubscribeToMessages("refreshSystemIndexes", handleSystemIndexes)

	<-stop
}

func handleSystemIndexes(msg *nats.Msg) {
	ctx := context.Background()
	etagKey := "systemIndexes-etags"
	existingETags, err := redisDB.GetETagsAsMap(ctx, etagKey)
	if err != nil {
		fmt.Printf("error retrieving map from redis: %v", etagKey)
	}

	allItems, newETags, err := esi.FetchSystemIndexes(existingETags)

	if err != nil {
		log.Printf("Error fetching system indexes: %v", err)
		return
	}

	for _, item := range allItems {
		key := fmt.Sprintf("solar_system:%d", item.SolarSystemID)
		err := redisDB.SaveAsJSON(ctx, key, item)
		if err != nil {
			log.Printf("Error saving to Redis: %v", err)
			return
		}
	}

	err = redisDB.SaveETagsAsHash(ctx, etagKey, newETags)
	if err != nil {
		fmt.Printf("error saving map to redis: %v", etagKey)
	}

	log.Printf("%v System Indexes Updated", len(allItems))
}

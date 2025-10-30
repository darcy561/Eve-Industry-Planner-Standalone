package main

import (
	"sync"

	"eveindustryplanner.com/esiworkers/apiServer"
	"eveindustryplanner.com/esiworkers/logger"
	"eveindustryplanner.com/esiworkers/messagebroker"
	"eveindustryplanner.com/esiworkers/redisDB"
	"eveindustryplanner.com/esiworkers/scheduler"
	"eveindustryplanner.com/esiworkers/worker"
)

func main() {
	logger.InitLogger()
	redisDB.Connect()
	messagebroker.Connect()
	scheduler.Start()
	defer scheduler.Stop()
	defer messagebroker.Disconnect()

	var wg sync.WaitGroup

	wg.Add(4)
	go worker.SubscribeAdjustedPrices(&wg)
	go worker.SubscribeMarketHistory(&wg)
	go worker.SubscribeMarketPrices(&wg)
	go worker.SubscribeSystemIndexes(&wg)
	go apiServer.StartServer(&wg)

	wg.Wait()
}

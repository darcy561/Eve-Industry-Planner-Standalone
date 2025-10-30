package endpoints

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"eveindustryplanner.com/esiworkers/apiServer/util"
	"eveindustryplanner.com/esiworkers/logger"
	"eveindustryplanner.com/esiworkers/redisDB"
)

const (
	maxWorkers = 5
)

func SystemIndexesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log := logger.GetLogger(r.Context(), logger.ApiProvider)

		log.Info("API Request Received")

		ids, err := util.ExtractIDs(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			log.Error(err.Error(),
				slog.Int("status_code", http.StatusBadRequest),
			)
			return
		}

		resultsChan := make(chan []byte, len(ids))

		go redisDataHandler_SystemIndexes(r.Context(), ids, resultsChan, log)

		results := collectRawJSONResults(resultsChan, len(ids))

		util.JsonResponse(w, results)
		util.LogRequestCompletion(r, log)
	}

}

func redisDataHandler_SystemIndexes(ctx context.Context, ids []string, results chan<- []byte, log *slog.Logger) {

	var wg sync.WaitGroup

	sem := make(chan struct{}, maxWorkers)

	for _, id := range ids {
		wg.Add(1)
		sem <- struct{}{}

		go func(id string) {
			defer func() {
				<-sem
				wg.Done()
			}()

			data, err := fetchSystemIndexFromRedis(ctx, id)
			if err != nil {
				log.Info(err.Error())
				return
			}

			results <- data

		}(id)
	}

	wg.Wait()

	close(results)

}

func fetchSystemIndexFromRedis(ctx context.Context, id string) ([]byte, error) {
	key := fmt.Sprintf("solar_system:%v", id)
	valueJSON, err := redisDB.GetValue(ctx, key)
	if err != nil {
		fmt.Printf("Error Getting Value: %v", err)
	}

	return []byte(valueJSON), nil
}

func collectRawJSONResults(rawJSONResults <-chan []byte, total int) []json.RawMessage {
	var results []json.RawMessage

	for range total {
		select {
		case rawJSON, ok := <-rawJSONResults:
			if ok {
				results = append(results, rawJSON)
			}
		}
	}

	return results
}

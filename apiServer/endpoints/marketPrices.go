package endpoints

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"eveindustryplanner.com/esiworkers/apiServer/util"
	"eveindustryplanner.com/esiworkers/esi"
	"eveindustryplanner.com/esiworkers/logger"
	"eveindustryplanner.com/esiworkers/redisDB"
	"eveindustryplanner.com/esiworkers/worker"
)

type LocationData struct {
	Locations     map[string]esi.DBMarketPriceEntry `json:"-"`
	LastUpdated   float64                           `json:"last_refreshed"` // Timestamp of when data was last refreshed
	TypeID        int64                             `json:"typeID"`
	AdjustedPrice float64                           `json:"AdjustedPrice"`
	AveragePrice  float64                           `json:"AveragePrice"`
}

const (
	maxIDs         = 500
	lastRefreshKey = "marketPrices_lastRefresh"
)

func MarketPricesHandler() http.HandlerFunc {
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

		dbResultsChan := make(chan LocationData, len(ids))
		missingIDsChan := make(chan string, len(ids))
		finalResultsChan := make(chan LocationData, len(ids))

		go redisDataHandler_MarketPrices(r.Context(), ids, dbResultsChan, missingIDsChan, log)

		go missingDataHandler(missingIDsChan, finalResultsChan)

		results := collectResults(dbResultsChan, finalResultsChan, len(ids))

		util.JsonResponse(w, results)
		util.LogRequestCompletion(r, log)
	}
}

func redisDataHandler_MarketPrices(ctx context.Context, ids []string, redisResults chan<- LocationData, missingIDs chan<- string, log *slog.Logger) {
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

			data, err := fetchFromRedis(ctx, id)
			if err != nil {
				log.Info(err.Error())
				missingIDs <- id
				return
			}

			redisResults <- data
		}(id)
	}
	wg.Wait()

	close(missingIDs)
}

func fetchFromRedis(ctx context.Context, id string) (LocationData, error) {
	lastUpdated, err := redisDB.GetScoreFromSortedSet(ctx, lastRefreshKey, id)
	if err != nil {
		return LocationData{}, fmt.Errorf("no data found for ID: %s. %v", id, err)
	}

	averagePrice, adjustedPrice := fetchAdjustedPrices(ctx, id)

	idAsInt, _ := strconv.ParseInt(id, 10, 64)

	typeData := LocationData{
		Locations:     make(map[string]esi.DBMarketPriceEntry),
		LastUpdated:   lastUpdated,
		TypeID:        idAsInt,
		AveragePrice:  averagePrice,
		AdjustedPrice: adjustedPrice,
	}

	for _, location := range esi.DEFAULT_MARKET_LOCATIONS {
		key := fmt.Sprintf("%v-marketPrices-%v", id, location.ID)
		data, err := redisDB.GetRedisMap(ctx, key)
		if err != nil {
			continue
		}

		var dbEntry esi.DBMarketPriceEntry
		if err := util.PopulateStruct(data, &dbEntry); err != nil {
			continue
		}

		typeData.Locations[strconv.Itoa(location.ID)] = dbEntry
	}

	return typeData, nil
}

func missingDataHandler(missingIDs <-chan string, results chan<- LocationData) {

	for id := range missingIDs {

		results <- fetchMissingData(id)
	}

	close(results)
}

func fetchMissingData(id string) LocationData {

	idAsInt, _ := strconv.ParseInt(id, 10, 64)

	averagePrice, adjustedPrice := fetchAdjustedPrices(context.Background(), id)

	typeData := LocationData{
		Locations:     make(map[string]esi.DBMarketPriceEntry),
		LastUpdated:   float64(time.Now().UnixMilli()),
		TypeID:        idAsInt,
		AveragePrice:  averagePrice,
		AdjustedPrice: adjustedPrice,
	}

	data := worker.FetchMarketPrices(context.Background(), int(idAsInt))

	typeData.Locations = data

	return typeData
}

func collectResults(redisResults <-chan LocationData, finalResults <-chan LocationData, total int) []LocationData {
	var results []LocationData

	for range total {
		select {
		case redisData, ok := <-redisResults:
			if ok {
				results = append(results, redisData)
			}
		case apiData, ok := <-finalResults:
			if ok {
				results = append(results, apiData)
			}
		}
	}

	return results
}

func fetchAdjustedPrices(ctx context.Context, id string) (float64, float64) {

	key := fmt.Sprintf("%v-adjustedPrice", id)

	data, err := redisDB.GetRedisMap(ctx, key)
	if err != nil {
		return 0, 0
	}

	var dbEntry esi.AdjustedPrice
	if err := util.PopulateStruct(data, &dbEntry); err != nil {

		return 0, 0
	}

	return dbEntry.AveragePrice, dbEntry.AdjustedPrice
}

func (ld *LocationData) MarshalJSON() ([]byte, error) {
	result := make(map[string]any)

	result["last_refreshed"] = ld.LastUpdated
	result["typeID"] = ld.TypeID
	result["adjustedPrices"] = ld.AdjustedPrice
	result["averagePrices"] = ld.AveragePrice

	for locID, priceData := range ld.Locations {
		result[string(locID)] = priceData
	}

	return json.Marshal(result)
}

package esi

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"time"

	"eveindustryplanner.com/esiworkers/shared/requests"
)

type ESIMarketHistoryEntry struct {
	Average    float64 `json:"average"`
	Date       string  `json:"date"`
	Highest    float64 `json:"highest"`
	Lowest     float64 `json:"lowest"`
	OrderCount int64   `json:"order_count"`
	Volume     int64   `json:"volume"`
}

type DBMarketHistoryEntry struct {
	DailyAverageMarketPrice   float64
	DailyAverageOrderQuantity float64
	DailyAverageUnitCount     float64
	HighestMarketPrice        float64
	LowestMarketPrice         float64
}

func FetchRegionHistory(MarketLocationData MarketLocation, Type_ID int, ETags map[int]string) (DBMarketHistoryEntry, map[int]string, error) {

	baseURL := &url.URL{
		Scheme: "https",
		Host:   "esi.evetech.net",
		Path:   fmt.Sprintf("/v1/markets/%d/history/", MarketLocationData.RegionID),
	}
	MaxQueryResponseLength := 500

	initialQueryParameters := url.Values{}
	initialQueryParameters.Add("datasource", "tranquility")
	initialQueryParameters.Add("type_id", strconv.Itoa(Type_ID))

	allItems, newETags, err := requests.FetchAllPages(*baseURL, initialQueryParameters, ETags, []ESIMarketHistoryEntry{}, MaxQueryResponseLength)

	if err != nil {
		return DBMarketHistoryEntry{}, newETags, fmt.Errorf("failed to fetch market history: %w", err)
	}

	filteredItems := filterEntiesOlderThan30Days(allItems)

	price, orders, units, high, low := processMarketData(filteredItems)

	return DBMarketHistoryEntry{
		DailyAverageMarketPrice:   price,
		DailyAverageOrderQuantity: orders,
		DailyAverageUnitCount:     units,
		HighestMarketPrice:        high,
		LowestMarketPrice:         low,
	}, newETags, nil
}

func filterEntiesOlderThan30Days(marketData []ESIMarketHistoryEntry) []ESIMarketHistoryEntry {
	days := 30
	dateFormat := "2006-01-02"
	thresholdTime := time.Now().AddDate(0, 0, -days)

	var filteredEntries []ESIMarketHistoryEntry

	for _, entry := range marketData {
		if parsedDate, err := time.Parse(dateFormat, entry.Date); err == nil && parsedDate.After(thresholdTime) {
			filteredEntries = append(filteredEntries, entry)
		}
	}

	return filteredEntries
}

func processMarketData(marketData []ESIMarketHistoryEntry) (avgPrice, avgOrders, avgUnits, high, low float64) {
	if len(marketData) == 0 {
		return 0, 0, 0, 0, 0
	}

	var totalOrders, totalUnits int64
	var totalAverage float64
	highest, lowest := math.Inf(-1), math.Inf(1)

	for _, entry := range marketData {
		totalAverage += entry.Average
		totalOrders += entry.OrderCount
		totalUnits += entry.Volume
		highest = max(highest, entry.Highest)
		lowest = min(lowest, entry.Lowest)
	}

	dataLength := float64(len(marketData))
	return roundToTwo(totalAverage / dataLength), roundToTwo(float64(totalOrders) / dataLength), roundToTwo(float64(totalUnits) / dataLength), highest, lowest
}

func roundToTwo(value float64) float64 {
	return math.Round(value*100) / 100
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

package esi

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"time"

	"eveindustryplanner.com/esiworkers/shared/requests"
)

type RegionMarketOrder struct {
	Duration     int32     `json:"duration"`
	IsBuyOrder   bool      `json:"is_buy_order"`
	Issued       time.Time `json:"issued"`
	LocationID   int64     `json:"location_id"`
	MinVolume    int32     `json:"min_volume"`
	OrderID      int64     `json:"order_id"`
	Price        float64   `json:"price"`
	Range        string    `json:"range"`
	SystemID     int32     `json:"system_id"`
	TypeID       int32     `json:"type_id"`
	VolumeRemain int32     `json:"volume_remain"`
	VolumeTotal  int32     `json:"volume_total"`
}

type DBMarketPriceEntry struct {
	TypeID int
	Buy    float64
	Sell   float64
}

func fetchRegionPrices(MarketLocationData MarketLocation, Type_ID int, ETags map[int]string) ([]RegionMarketOrder, map[int]string, error) {

	baseURL := &url.URL{
		Scheme: "https",
		Host:   "esi.evetech.net",
		Path:   fmt.Sprintf("/v1/markets/%d/orders/", MarketLocationData.RegionID),
	}
	MaxQueryResponseLength := 1000

	queryParams := url.Values{
		"datasource": {"tranquility"},
		"order_type": {"all"},
		"type_id":    {strconv.Itoa(Type_ID)},
		"page":       {"1"},
	}

	allOrders, newETags, err := requests.FetchAllPages(*baseURL, queryParams, ETags, []RegionMarketOrder{}, MaxQueryResponseLength)
	if err != nil {
		return nil, newETags, fmt.Errorf("failed to fetch market orders: %w", err)
	}

	return allOrders, newETags, nil
}

func FetchStationPrices(MarketLocationData MarketLocation, Type_ID int, ETags map[int]string) (DBMarketPriceEntry, map[int]string, error) {

	allOrders, newETags, err := fetchRegionPrices(MarketLocationData, Type_ID, ETags)
	if err != nil {
		fmt.Println(err)
		return DBMarketPriceEntry{}, newETags, err
	}

	stationOrders := filterOrdersByStation(allOrders, MarketLocationData.StationID)
	buyOrders, sellOrders := categorizeOrders(stationOrders)
	highestBuyPrice, lowestSellPrice := getBestBuyAndSellPrices(buyOrders, sellOrders)

	return DBMarketPriceEntry{
		TypeID: Type_ID,
		Buy:    highestBuyPrice,
		Sell:   lowestSellPrice,
	}, newETags, nil
}

func filterOrdersByStation(orders []RegionMarketOrder, stationID int) []RegionMarketOrder {
	filtered := make([]RegionMarketOrder, 0, len(orders))
	for _, order := range orders {
		if order.LocationID == int64(stationID) {
			filtered = append(filtered, order)
		}
	}
	return filtered
}

func categorizeOrders(orders []RegionMarketOrder) (buyOrders, sellOrders []RegionMarketOrder) {
	buyOrders, sellOrders = make([]RegionMarketOrder, 0, len(orders)/2), make([]RegionMarketOrder, 0, len(orders)/2)

	for _, order := range orders {
		if order.IsBuyOrder {
			buyOrders = append(buyOrders, order)
		} else {
			sellOrders = append(sellOrders, order)
		}
	}
	return
}

func getBestBuyAndSellPrices(buyOrders, sellOrders []RegionMarketOrder) (highestBuyPrice, lowestSellPrice float64) {
	highestBuyPrice = 0
	lowestSellPrice = math.MaxFloat64

	for _, order := range buyOrders {
		if order.Price > highestBuyPrice {
			highestBuyPrice = order.Price
		}
	}

	for _, order := range sellOrders {
		if order.Price < lowestSellPrice {
			lowestSellPrice = order.Price
		}
	}

	if lowestSellPrice == math.MaxFloat64 {
		lowestSellPrice = 0
	}

	return
}

package esi

import (
	"fmt"
	"net/url"

	"eveindustryplanner.com/esiworkers/shared/requests"
)

type AdjustedPrice struct {
	AdjustedPrice float64 `json:"adjusted_price"`
	AveragePrice  float64 `json:"average_price"`
	TypeID        int32   `json:"type_id"`
}

// Fetchs the Adjusted Price Data from the Eve ESI. Returns a slice.
func FetchAdjustedPrices(existingETags map[int]string) ([]AdjustedPrice, map[int]string, error) {
	baseURL := &url.URL{
		Scheme: "https",
		Host:   "esi.evetech.net",
		Path:   "/v1/markets/prices/",
	}
	queryParams := url.Values{}

	allItems, newETags, err := requests.FetchAllPages(*baseURL, queryParams, existingETags, []AdjustedPrice{}, 20000)

	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch adjusted prices: %w", err)
	}

	return allItems, newETags, nil
}

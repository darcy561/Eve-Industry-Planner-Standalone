package esi

import (
	"fmt"
	"net/url"
	"time"

	"eveindustryplanner.com/esiworkers/shared/requests"
)

// Cost Indice represents an individual cost index.
type ESICostIndice struct {
	Activity  string  `json:"activity"`
	CostIndex float64 `json:"cost_index"`
}

// Industry System represents the structure for each item in the list.
type ESIIndustrySystem struct {
	CostIndices   []ESICostIndice `json:"cost_indices"`
	SolarSystemID int32           `json:"solar_system_id"`
}

type SystemIndexes struct {
	SolarSystemID    int32   `json:"solar_system_id"`
	LastUpdated      int64   `json:"lastUpdated"`
	Manufacturing    float64 `json:"manufacturing,omitempty"`
	ResearchTime     float64 `json:"researching_time_efficiency,omitempty"`
	ResearchMaterial float64 `json:"researching_material_efficiency,omitempty"`
	Copying          float64 `json:"copying,omitempty"`
	Invention        float64 `json:"invention,omitempty"`
	Reaction         float64 `json:"reaction,omitempty"`
}

func FetchSystemIndexes(existingETags map[int]string) ([]SystemIndexes, map[int]string, error) {

	baseURL := &url.URL{
		Scheme: "https",
		Host:   "esi.evetech.net",
		Path:   "/v1/industry/systems/",
	}

	queryParams := url.Values{}
	if tag, exists := existingETags[1]; exists {
		queryParams.Set("If-None-Match", tag)
	}

	baseURL.RawQuery = queryParams.Encode()

	newETags := make(map[int]string)

	allItems, headers, err := requests.MakeHttpRequest(baseURL.String(), nil, queryParams, []ESIIndustrySystem{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch system indexes: %w", err)
	}

	newETags[1] = headers.Get("ETag")

	results := make([]SystemIndexes, 0, len(allItems))

	for _, data := range allItems {
		systemIndex := SystemIndexes{
			SolarSystemID: data.SolarSystemID,
			LastUpdated:   time.Now().UnixMilli(),
		}

		for _, costIndex := range data.CostIndices {
			switch costIndex.Activity {
			case "manufacturing":
				systemIndex.Manufacturing = costIndex.CostIndex
			case "researching_time_efficiency":
				systemIndex.ResearchTime = costIndex.CostIndex
			case "researching_material_efficiency":
				systemIndex.ResearchMaterial = costIndex.CostIndex
			case "copying":
				systemIndex.Copying = costIndex.CostIndex
			case "invention":
				systemIndex.Invention = costIndex.CostIndex
			case "reaction":
				systemIndex.Reaction = costIndex.CostIndex
			}

		}
		results = append(results, systemIndex)
	}

	return results, newETags, nil
}

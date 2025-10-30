package esi

type MarketLocation struct {
	ID        int
	Name      string
	RegionID  int
	StationID int
}

var DEFAULT_MARKET_LOCATIONS = []MarketLocation{
	{ID: 1, Name: "Jita", RegionID: 10000002, StationID: 60003760},
	{ID: 2, Name: "Amarr", RegionID: 10000043, StationID: 60008494},
	{ID: 3, Name: "Dodixie", RegionID: 10000032, StationID: 60011866},
}

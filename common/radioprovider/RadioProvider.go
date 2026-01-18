package radioprovider

type Country struct {
	Name         string `json:"name"`
	Id           string `json:"iso_3166_1"`
	StationCount int    `json:"stationcount"`
}

type Station struct {
	Name      string `json:"name"`
	Id        string `json:"stationuuid"`
	StreamUrl string `json:"url_resolved"`
	Codec     string `json:"codec"`
	Bitrate   int    `json:"bitrate"`
	Homepage  string `json:"homepage"`
	Country   string `json:"country"`
	Genre     string `json:"tags"`
}

type RadioProvider interface {
	GetCountries() ([]Country, error)
	GetStationsByCountry(countryId string) ([]Station, error)
	GetMostPopularStations(count int) ([]Station, error)
	GetMostLikedStations(count int) ([]Station, error)
	GetStationById(stationId string) (Station, error)
	SearchStations(search string) ([]Station, error)
}

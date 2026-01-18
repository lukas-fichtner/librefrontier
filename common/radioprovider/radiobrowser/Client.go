package radiobrowser

import (
	"encoding/json"
	"github.com/compujuckel/librefrontier/common/radioprovider"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"gopkg.in/resty.v1"
	"net/url"
	"strconv"
	"strings"
)

type Client struct {
	db interface {
		GetStationByTruncatedUUID(string) (string, bool)
	}
}

func NewRadioBrowserClient(db interface {
	GetStationByTruncatedUUID(string) (string, bool)
}) radioprovider.RadioProvider {
	// Set a speaking User-Agent per RadioBrowser guidelines
	resty.SetHeader("User-Agent", "LibreFrontier/0.1 (+https://github.com/compujuckel/librefrontier)")
	return &Client{db: db}
}

var _ radioprovider.RadioProvider = (*Client)(nil)

func (r *Client) GetCountries() ([]radioprovider.Country, error) {
	url := "https://de1.api.radio-browser.info/json/countries"
	log.Debugf("Fetching countries from: %s", url)

	resp, err := resty.R().Get(url)
	if err != nil {
		return nil, errors.Wrap(err, "get countries")
	}
	if resp.IsError() {
		return nil, errors.Errorf("get countries: status %d body %s", resp.StatusCode(), string(resp.Body()))
	}

	log.Debugf("Countries response status: %d, body length: %d", resp.StatusCode(), len(resp.Body()))

	var countries []radioprovider.Country

	err = json.Unmarshal(resp.Body(), &countries)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshal countries")
	}

	log.Debugf("Result: %v", countries)

	return countries, nil
}

func (r *Client) GetStationsByCountry(countryId string) ([]radioprovider.Station, error) {
	url := "https://de1.api.radio-browser.info/json/stations/bycountrycodeexact/" + countryId
	log.Debugf("Fetching stations by country from: %s", url)

	resp, err := resty.R().Get(url)
	if err != nil {
		return nil, errors.Wrap(err, "get stations")
	}
	if resp.IsError() {
		return nil, errors.Errorf("get stations: status %d body %s", resp.StatusCode(), string(resp.Body()))
	}

	log.Debugf("Stations by country response status: %d, body length: %d", resp.StatusCode(), len(resp.Body()))

	var stations []radioprovider.Station

	err = json.Unmarshal(resp.Body(), &stations)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshal stations")
	}

	log.Debugf("Result: %v", stations)

	return stations, nil
}

func (r *Client) GetMostPopularStations(count int) ([]radioprovider.Station, error) {
	url := "https://de1.api.radio-browser.info/json/stations/topclick/" + strconv.Itoa(count)
	log.Debugf("Fetching most popular stations from: %s", url)

	resp, err := resty.R().Get(url)
	if err != nil {
		return nil, errors.Wrap(err, "get stations")
	}
	if resp.IsError() {
		return nil, errors.Errorf("get stations: status %d body %s", resp.StatusCode(), string(resp.Body()))
	}

	log.Debugf("Popular stations response status: %d, body length: %d", resp.StatusCode(), len(resp.Body()))

	var stations []radioprovider.Station

	err = json.Unmarshal(resp.Body(), &stations)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshal stations")
	}

	log.Debugf("Result: %v", stations)

	return stations, nil
}

func (r *Client) GetMostLikedStations(count int) ([]radioprovider.Station, error) {
	url := "https://de1.api.radio-browser.info/json/stations/topvote/" + strconv.Itoa(count)
	log.Debugf("Fetching most liked stations from: %s", url)

	resp, err := resty.R().Get(url)
	if err != nil {
		return nil, errors.Wrap(err, "get stations")
	}
	if resp.IsError() {
		return nil, errors.Errorf("get stations: status %d body %s", resp.StatusCode(), string(resp.Body()))
	}

	log.Debugf("Liked stations response status: %d, body length: %d", resp.StatusCode(), len(resp.Body()))

	var stations []radioprovider.Station

	err = json.Unmarshal(resp.Body(), &stations)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshal stations")
	}

	log.Debugf("Result: %v", stations)

	return stations, nil
}

func (r *Client) SearchStations(search string) ([]radioprovider.Station, error) {
	url := "https://de1.api.radio-browser.info/json/stations/byname/" + url.PathEscape(search)
	log.Debugf("Searching stations from: %s", url)

	resp, err := resty.R().Get(url)
	if err != nil {
		return nil, errors.Wrap(err, "get stations")
	}
	if resp.IsError() {
		return nil, errors.Errorf("get stations: status %d body %s", resp.StatusCode(), string(resp.Body()))
	}

	log.Debugf("Search stations response status: %d, body length: %d", resp.StatusCode(), len(resp.Body()))

	var stations []radioprovider.Station

	err = json.Unmarshal(resp.Body(), &stations)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshal stations")
	}

	log.Debugf("Result: %v", stations)

	return stations, nil
}

func (r *Client) GetStationById(stationId string) (radioprovider.Station, error) {
	byUUID := "https://de1.api.radio-browser.info/json/stations/byuuid/" + stationId
	log.Debugf("Fetching station by ID from: %s", byUUID)

	resp, err := resty.R().Get(byUUID)
	if err != nil {
		return radioprovider.Station{}, errors.Wrap(err, "get station")
	}
	if resp.IsError() {
		return radioprovider.Station{}, errors.Errorf("get station: status %d body %s", resp.StatusCode(), string(resp.Body()))
	}

	log.Debugf("Station by ID response status: %d, body length: %d", resp.StatusCode(), len(resp.Body()))

	var stations []radioprovider.Station

	err = json.Unmarshal(resp.Body(), &stations)
	if err != nil {
		return radioprovider.Station{}, errors.Wrap(err, "unmarshal station")
	}

	log.Debugf("Result: %v", stations)

	if len(stations) > 0 {
		return stations[0], nil
	}

	// Some devices send truncated UUIDs. Try database lookup for cached full UUID.
	if len(stationId) >= 32 && len(stationId) < 36 && strings.Count(stationId, "-") == 4 {
		if r.db != nil {
			if fullUUID, found := r.db.GetStationByTruncatedUUID(stationId); found {
				log.Infof("Found full UUID in database for truncated UUID %s: %s", stationId, fullUUID)
				return r.GetStationById(fullUUID)
			}
		}
		// Heuristic: many station UUIDs end with '52543be04c81'. If final block is 8 chars and starts with '52543be0', try completing with '4c81'.
		parts := strings.Split(stationId, "-")
		if len(parts) == 5 {
			last := parts[4]
			if len(last) == 8 && strings.HasPrefix(last, "52543be0") {
				candidate := parts[0] + "-" + parts[1] + "-" + parts[2] + "-" + parts[3] + "-" + last + "4c81"
				log.Infof("Attempting heuristic completion for truncated UUID %s -> %s", stationId, candidate)
				byUUID2 := "https://de1.api.radio-browser.info/json/stations/byuuid/" + candidate
				resp2, err2 := resty.R().Get(byUUID2)
				if err2 == nil && !resp2.IsError() {
					var stations2 []radioprovider.Station
					if json.Unmarshal(resp2.Body(), &stations2) == nil && len(stations2) > 0 {
						log.Infof("Heuristic completion resolved station: %s", stations2[0].Name)
						return stations2[0], nil
					}
				}
			}
		}
		log.Warnf("UUID truncated (len=%d, missing %d chars): %s - station not in database, cannot resolve", 
			len(stationId), 36-len(stationId), stationId)
	}

	// Some legacy devices send the numeric station ID. Only fallback to the byid endpoint when the ID is numeric.
	if isNumeric(stationId) {
		byID := "https://de1.api.radio-browser.info/json/stations/byid/" + stationId
		log.Debugf("UUID lookup empty; retrying legacy byid endpoint: %s", byID)

		resp, err = resty.R().Get(byID)
		if err != nil {
			return radioprovider.Station{}, errors.Wrap(err, "get station byid")
		}
		if resp.IsError() {
			return radioprovider.Station{}, errors.Errorf("get station byid: status %d body %s", resp.StatusCode(), string(resp.Body()))
		}

		log.Debugf("Station by ID (legacy) response status: %d, body length: %d", resp.StatusCode(), len(resp.Body()))

		err = json.Unmarshal(resp.Body(), &stations)
		if err != nil {
			return radioprovider.Station{}, errors.Wrap(err, "unmarshal station byid")
		}

		log.Debugf("Legacy byid result: %v", stations)

		if len(stations) > 0 {
			return stations[0], nil
		}
	} else {
		log.Debugf("Provided stationId is not numeric; skipping legacy byid fallback: %s", stationId)
	}

	return radioprovider.Station{}, errors.New("No station found")
}

// isNumeric reports whether s consists only of digits.
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

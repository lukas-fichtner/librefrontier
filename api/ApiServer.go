package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"github.com/compujuckel/librefrontier/common"
	"github.com/compujuckel/librefrontier/common/radioprovider"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"go.uber.org/fx"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

type ApiServer struct {
	db         *common.Database
	cfg        *common.Config
	xml        *common.XmlBuilder
	gin        *gin.Engine
	radio      radioprovider.RadioProvider
	httpClient *http.Client
}

type DeviceInfo struct {
	Mac      string `form:"mac" binding:"required"`
	Language string `form:"dlang"`
	Fver     string `form:"fver"`
	Vendor   string `form:"ven"`
}

type PaginatedRequest struct {
	Device *DeviceInfo
	Start  int `form:"startItems"`
	End    int `form:"endItems"`
}

type SearchRequestSingle struct {
	Device     *DeviceInfo
	SearchType int    `form:"sSearchtype" binding:"required"`
	Search     string `form:"Search" binding:"required"`
}

type SearchRequest struct {
	Device     *DeviceInfo
	Start      int    `form:"startItems"`
	End        int    `form:"endItems"`
	SearchType int    `form:"sSearchtype" binding:"required"`
	Search     string `form:"search" binding:"required"`
}

func NewApiController(lc fx.Lifecycle, config *common.Config, database *common.Database, xmlBuilder *common.XmlBuilder, radioProvider radioprovider.RadioProvider) *ApiServer {
	a := ApiServer{}
	a.cfg = config
	a.db = database
	a.xml = xmlBuilder
	a.radio = radioProvider
	a.gin = gin.Default()
	a.httpClient = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig:    &tls.Config{InsecureSkipVerify: false},
			DisableCompression: true,
		},
	}

	a.gin.GET("/setupapp/karcher/asp/BrowseXML/loginXML.asp", a.fsLoginXML)
	a.gin.GET("/setupapp/karcher/asp/BrowseXML/Search.asp", a.fsSearch)
	a.gin.GET("/countries", a.getCountries)
	a.gin.GET("/country/:country", a.getStationsByCountry)
	a.gin.GET("/stations/popular", a.getMostPopularStations)
	a.gin.GET("/stations/liked", a.getMostLikedStations)
	a.gin.GET("/stations/search", a.searchStations)
	a.gin.GET("/station/:station/play", a.getStreamUrl)
	a.gin.GET("/proxy/stream", a.proxyStream)
	a.gin.GET("/station/:station", a.getStationDetail)
	a.gin.GET("/favorite/add/:station", a.addFavorite)
	a.gin.GET("/favorite/remove/:station", a.removeFavorite)
	a.gin.GET("/favorites", a.getFavorites)
	a.gin.GET("/empty", a.getEmpty)

	server := http.Server{
		Addr:    ":80",
		Handler: a.gin,
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			log.Info("Starting HTTP server.")
			// In production, we'd want to separate the Listen and Serve phases for
			// better error-handling.
			go server.ListenAndServe()

			return nil
		},
		OnStop: func(ctx context.Context) error {
			log.Info("Stopping HTTP server.")
			return server.Shutdown(ctx)
		},
	})

	return &a
}

func (a *ApiServer) fsLoginXML(c *gin.Context) {
	log.Printf("fs_loginXML")

	base := a.cfg.GetApiBaseUrl()
	if base == "" {
		proto := c.Request.Header.Get("X-Forwarded-Proto")
		if proto == "" {
			if c.Request.TLS != nil {
				proto = "https"
			} else {
				proto = "http"
			}
		}
		base = proto + "://" + c.Request.Host
		a.cfg.SetApiBaseUrl(base)
		log.Infof("Derived api base URL from request: %s", base)
	}

	if c.Query("token") == "0" {
		// This is a Frontier Silicon device authentication token response
		// The token is used for device authentication and session management
		// TODO investigate how this is used
		c.String(http.StatusOK, "<EncryptedToken>3a3f5ac48a1dab4e</EncryptedToken>")
		return
	}

	items := []common.Item{
		{
			ItemType:     "Dir",
			Title:        "Favorites",
			UrlDir:       a.cfg.GetApiBaseUrl() + "/favorites",
			UrlDirBackUp: a.cfg.GetApiBaseUrl() + "/favorites",
		}, {
			ItemType:     "Dir",
			Title:        "By Country",
			UrlDir:       a.cfg.GetApiBaseUrl() + "/countries",
			UrlDirBackUp: a.cfg.GetApiBaseUrl() + "/countries",
		}, {
			ItemType:     "Dir",
			Title:        "Most popular",
			UrlDir:       a.cfg.GetApiBaseUrl() + "/stations/popular",
			UrlDirBackUp: a.cfg.GetApiBaseUrl() + "/stations/popular",
		}, {
			ItemType:     "Dir",
			Title:        "Most liked",
			UrlDir:       a.cfg.GetApiBaseUrl() + "/stations/liked",
			UrlDirBackUp: a.cfg.GetApiBaseUrl() + "/stations/liked",
		}, {
			ItemType:        "Search",
			SearchURL:       a.cfg.GetApiBaseUrl() + "/stations/search?sSearchtype=2",
			SearchURLBackUp: a.cfg.GetApiBaseUrl() + "/stations/search?sSearchtype=2",
			SearchCaption:   "Search stations",
			SearchTextbox:   "",
			SearchGo:        "Search",
			SearchCancel:    "%search-cancel%",
		}, {
			ItemType:     "Dir",
			Title:        "LibreFrontier PoC",
			UrlDir:       a.cfg.GetApiBaseUrl() + "/empty",
			UrlDirBackUp: a.cfg.GetApiBaseUrl() + "/empty",
		},
	}

	menu := common.ListOfItems{
		ItemCount: len(items),
		Items:     items,
	}

	// sadly we cannot use c.XML here because it does not write the XML header
	a.xml.WriteToWire(c.Writer, menu)
}

func (a *ApiServer) fsSearch(c *gin.Context) {
	var r SearchRequestSingle

	err := c.Bind(&r)
	if err != nil {
		log.Warnf("fsSearch bind error: %v", err)
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	a.db.CreateDevice(r.Device.Mac)

	log.Debugf("fsSearch: mac=%s, Search=%s, sSearchtype=%d", r.Device.Mac, r.Search, r.SearchType)

	station, err := a.radio.GetStationById(r.Search)
	if err != nil {
		log.Errorf("Failed to get station by ID %s: %v", r.Search, err)
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	
	// Cache the station in the database for truncated UUID resolution
	a.db.CacheStation(station.Id, station.Name)
	
	log.Debugf("Found station: %s (ID: %s)", station.Name, station.Id)
	list := a.xml.CreateStationsList([]radioprovider.Station{station}, 0, 0, true)

	a.xml.WriteToWire(c.Writer, list)
}

func (a *ApiServer) getEmpty(c *gin.Context) {
	a.xml.WriteToWire(c.Writer, common.ListOfItems{})
}

func (a *ApiServer) getCountries(c *gin.Context) {
	var p PaginatedRequest
	err := c.Bind(&p)
	if err != nil {
		log.Warnf("getCountries bind error: %v", err)
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	if p.Start == 0 && p.End == 0 {
		p.Start = 1
		p.End = 50
	}

	log.Debugf("getCountries called: mac=%s, start=%d, end=%d", p.Device.Mac, p.Start, p.End)

	countries, err := a.radio.GetCountries()
	if err != nil {
		log.Errorf("Failed to get countries: %v", err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	log.Debugf("Retrieved %d countries", len(countries))
	list := a.xml.CreateCountryList(countries, p.Start-1, p.End)

	a.xml.WriteToWire(c.Writer, list)
}

func (a *ApiServer) getStationsByCountry(c *gin.Context) {
	var p PaginatedRequest
	err := c.Bind(&p)
	if err != nil {
		log.Warnf("getStationsByCountry bind error: %v", err)
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	if p.Start == 0 && p.End == 0 {
		p.Start = 1
		p.End = 50
	}

	countryCode := c.Param("country")
	log.Debugf("getStationsByCountry called: country=%s, mac=%s, start=%d, end=%d", countryCode, p.Device.Mac, p.Start, p.End)

	stations, err := a.radio.GetStationsByCountry(countryCode)
	if err != nil {
		log.Errorf("Failed to get stations for country %s: %v", countryCode, err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	log.Debugf("Retrieved %d stations for country %s", len(stations), countryCode)
	list := a.xml.CreateStationsList(stations, p.Start-1, p.End, false)

	a.xml.WriteToWire(c.Writer, list)
}

func (a *ApiServer) getMostPopularStations(c *gin.Context) {
	var p PaginatedRequest
	err := c.Bind(&p)
	if err != nil {
		log.Warnf("getMostPopularStations bind error: %v", err)
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	if p.Start == 0 && p.End == 0 {
		p.Start = 1
		p.End = 50
	}

	log.Debugf("getMostPopularStations called: mac=%s, start=%d, end=%d", p.Device.Mac, p.Start, p.End)

	stations, err := a.radio.GetMostPopularStations(100)
	if err != nil {
		log.Errorf("Failed to get popular stations: %v", err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	log.Debugf("Retrieved %d popular stations", len(stations))
	list := a.xml.CreateStationsList(stations, p.Start-1, p.End, false)

	a.xml.WriteToWire(c.Writer, list)
}

func (a *ApiServer) getMostLikedStations(c *gin.Context) {
	var p PaginatedRequest
	err := c.Bind(&p)
	if err != nil {
		log.Warnf("getMostLikedStations bind error: %v", err)
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	if p.Start == 0 && p.End == 0 {
		p.Start = 1
		p.End = 50
	}

	log.Debugf("getMostLikedStations called: mac=%s, start=%d, end=%d", p.Device.Mac, p.Start, p.End)

	stations, err := a.radio.GetMostLikedStations(100)
	if err != nil {
		log.Errorf("Failed to get liked stations: %v", err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	log.Debugf("Retrieved %d liked stations", len(stations))
	list := a.xml.CreateStationsList(stations, p.Start-1, p.End, false)

	a.xml.WriteToWire(c.Writer, list)
}

func (a *ApiServer) searchStations(c *gin.Context) {
	var s SearchRequest
	err := c.Bind(&s)
	if err != nil {
		log.Warnf("searchStations bind error: %v", err)
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	if s.Start == 0 && s.End == 0 {
		s.Start = 1
		s.End = 50
	}

	log.Debugf("searchStations called: search=%s, mac=%s, start=%d, end=%d", s.Search, s.Device.Mac, s.Start, s.End)

	stations, err := a.radio.SearchStations(s.Search)
	if err != nil {
		log.Errorf("Failed to search stations for '%s': %v", s.Search, err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	log.Debugf("Search '%s' returned %d stations", s.Search, len(stations))
	list := a.xml.CreateStationsList(stations, s.Start-1, s.End, false)

	a.xml.WriteToWire(c.Writer, list)
}

func (a *ApiServer) getStationDetail(c *gin.Context) {
	var d DeviceInfo
	err := c.Bind(&d)
	if err != nil {
		log.Warnf("getStationDetail bind error: %v", err)
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	stationId := c.Param("station")
	log.Debugf("getStationDetail called: stationId=%s, mac=%s", stationId, d.Mac)

	station, err := a.radio.GetStationById(stationId)
	if err != nil {
		log.Errorf("Failed to get station %s: %v", stationId, err)
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	// Cache the station in the database for truncated UUID resolution
	a.db.CacheStation(station.Id, station.Name)

	fav := a.db.IsFavorite(d.Mac, station.Id)
	log.Debugf("Station %s (%s) favorite status for %s: %v", station.Name, station.Id, d.Mac, fav)
	list := a.xml.CreateStationDetail(station, fav)

	a.xml.WriteToWire(c.Writer, list)
}

func (a *ApiServer) getStreamUrl(c *gin.Context) {
	stationId := c.Param("station")
	log.Debugf("getStreamUrl called: stationId=%s", stationId)

	station, err := a.radio.GetStationById(stationId)
	if err != nil {
		log.Errorf("Failed to get stream URL for station %s: %v", stationId, err)
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	// Check if stream URL is HTTPS
	if strings.HasPrefix(strings.ToLower(station.StreamUrl), "https://") {
		// Create proxy URL for HTTPS streams
		proxyUrl := fmt.Sprintf("%s/proxy/stream?url=%s", a.cfg.GetApiBaseUrl(), url.QueryEscape(station.StreamUrl))
		log.Debugf("Stream is HTTPS, returning proxy URL for %s: %s", station.Name, proxyUrl)
		c.String(http.StatusOK, proxyUrl)
	} else {
		// Return HTTP stream URL as-is
		log.Debugf("Returning HTTP stream URL for %s: %s", station.Name, station.StreamUrl)
		c.String(http.StatusOK, station.StreamUrl)
	}
}

func (a *ApiServer) proxyStream(c *gin.Context) {
	streamUrl := c.Query("url")
	if streamUrl == "" {
		log.Warn("proxyStream called without url parameter")
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	if !isAllowedStreamURL(streamUrl) {
		log.Warnf("proxyStream rejected disallowed URL: %s", streamUrl)
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	log.Debugf("Proxying HTTPS stream: %s", streamUrl)

	// Request the HTTPS stream
	req, err := http.NewRequest("GET", streamUrl, nil)
	if err != nil {
		log.Errorf("Failed to create request for %s: %v", streamUrl, err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	// Copy all headers from the original request to preserve ICY metadata and other important headers
	for key, values := range c.Request.Header {
		// Skip host header as it should be set to the upstream server
		if strings.ToLower(key) == "host" {
			continue
		}
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		log.Errorf("Failed to fetch stream from %s: %v", streamUrl, err)
		c.AbortWithStatus(http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		log.Warnf("Stream returned non-OK status %d for %s", resp.StatusCode, streamUrl)
		c.AbortWithStatus(resp.StatusCode)
		return
	}

	// Set status first, before any headers
	c.Status(http.StatusOK)
	
	// Copy response headers to preserve ICY metadata and content type
	for key, values := range resp.Header {
		for _, value := range values {
			c.Header(key, value)
		}
	}

	// Flush headers to client
	c.Writer.Flush()

	// Stream the content
	_, err = io.Copy(c.Writer, resp.Body)
	if err != nil {
		log.Debugf("Stream copy completed/interrupted for %s: %v", streamUrl, err)
	}
}

func (a *ApiServer) addFavorite(c *gin.Context) {
	var d DeviceInfo
	err := c.Bind(&d)
	if err != nil {
		log.Warnf("addFavorite bind error: %v", err)
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	station, err := a.radio.GetStationById(c.Param("station"))
	if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	a.db.AddFavorite(d.Mac, station.Id, station.Name)
	log.Infof("Added favorite %s for mac %s", station.Name, d.Mac)

	list := a.xml.CreateStationDetail(station, true)

	a.xml.WriteToWire(c.Writer, list)
}

func (a *ApiServer) removeFavorite(c *gin.Context) {
	var d DeviceInfo
	err := c.Bind(&d)
	if err != nil {
		log.Warnf("removeFavorite bind error: %v", err)
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	station, err := a.radio.GetStationById(c.Param("station"))
	if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	a.db.RemoveFavorite(d.Mac, station.Id)
	log.Infof("Removed favorite %s for mac %s", station.Name, d.Mac)

	list := a.xml.CreateStationDetail(station, false)

	a.xml.WriteToWire(c.Writer, list)
}

func (a *ApiServer) getFavorites(c *gin.Context) {
	var p PaginatedRequest
	err := c.Bind(&p)
	if err != nil {
		log.Warnf("getFavorites bind error: %v", err)
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	if p.Start == 0 && p.End == 0 {
		p.Start = 1
		p.End = 50
	}

	stations := a.db.GetFavoriteStations(p.Device.Mac)

	list := a.xml.CreateStationsList(stations, p.Start-1, p.End, false)

	a.xml.WriteToWire(c.Writer, list)
}

// isAllowedStreamURL validates that a URL is safe to proxy: only http/https
// and not pointing at private/loopback addresses (SSRF prevention).
func isAllowedStreamURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	host := u.Hostname()
	if host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		for _, cidr := range []string{
			"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12",
			"192.168.0.0/16", "169.254.0.0/16", "0.0.0.0/8",
			"::1/128", "fc00::/7", "fe80::/10",
		} {
			_, network, _ := net.ParseCIDR(cidr)
			if network != nil && network.Contains(ip) {
				return false
			}
		}
	}
	lower := strings.ToLower(host)
	return lower != "localhost" &&
		!strings.HasSuffix(lower, ".local") &&
		!strings.HasSuffix(lower, ".internal") &&
		!strings.HasSuffix(lower, ".localhost")
}

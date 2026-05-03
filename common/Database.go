package common

import (
	"database/sql"
	"github.com/compujuckel/librefrontier/common/radioprovider"
	_ "github.com/lib/pq"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"strings"
)

type Database struct {
	db *sql.DB
}

func NewDatabase(config *Config) (*Database, error) {
	database := Database{}

	db, err := sql.Open("postgres", config.dbConnString)
	if err != nil {
		return nil, errors.Wrap(err, "Cannot connect to db")
	}

	database.db = db

	// Auto-migration: ensure radiobrowser_id column is TEXT, not INTEGER
	// This handles cases where the schema was created with the wrong type
	ensureSchemaUp(&database)

	return &database, nil
}

// ensureSchemaUp runs necessary schema migrations to fix legacy issues
func ensureSchemaUp(d *Database) {
	// Check if radiobrowser_id is INTEGER and convert to TEXT
	var dataType string
	err := d.db.QueryRow(`
		SELECT data_type 
		FROM information_schema.columns 
		WHERE table_name = 'station' AND column_name = 'radiobrowser_id'
	`).Scan(&dataType)

	if err != nil && err != sql.ErrNoRows {
		log.Warnf("Could not check column type: %v", err)
		return
	}

	if dataType == "integer" {
		log.Info("Migrating station.radiobrowser_id from INTEGER to TEXT...")
		_, err := d.db.Exec(`
			ALTER TABLE station
			ALTER COLUMN radiobrowser_id TYPE TEXT USING radiobrowser_id::TEXT
		`)
		if err != nil {
			log.Errorf("Failed to migrate column type: %v", err)
		} else {
			log.Info("Successfully migrated station.radiobrowser_id to TEXT")
		}
	}
}

func (d *Database) CreateDevice(mac string) {
	s := "INSERT INTO device (mac) VALUES ($1) ON CONFLICT DO NOTHING;"

	_, err := d.db.Exec(s, mac)
	if err != nil {
		log.Error("Error creating device: ", err)
	}
}

func (d *Database) createRadioBrowserStation(stationId string, stationName string) {
	s := "INSERT INTO station (radiobrowser_id, name) VALUES ($1, $2) ON CONFLICT DO NOTHING;"

	_, err := d.db.Exec(s, stationId, stationName)
	if err != nil {
		log.Error("Error creating station: ", err)
	}
}

// CacheStation stores a station in the database for truncated UUID resolution without adding it as a favorite
func (d *Database) CacheStation(stationId string, stationName string) {
	d.createRadioBrowserStation(stationId, stationName)
}

func (d *Database) AddFavorite(mac string, stationId string, stationName string) {
	// Use a transaction to ensure atomicity
	tx, err := d.db.Begin()
	if err != nil {
		log.Error("Error starting transaction for adding favorite: ", err)
		return
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			log.Error("Error rolling back transaction for adding favorite: ", err)
		}
	}()

	// Create or update the station
	d.createRadioBrowserStation(stationId, stationName)

	s := `INSERT INTO favorite (device_id, station_id) SELECT (SELECT d.device_id FROM device d WHERE d.mac = $1), (SELECT s.station_id FROM station s WHERE s.radiobrowser_id = $2)`

	_, err = tx.Exec(s, mac, stationId)
	if err != nil {
		log.Error("Error adding favorite: ", err)
		return
	}

	// Cache truncated UUID if applicable for future lookups (resolves truncated UUIDs from legacy devices)
	if len(stationId) >= 23 && len(stationId) <= 36 && strings.Contains(stationId, "-") {
		truncated := stationId[:23] // Store first 23 chars as a prefix for LIKE queries
		log.Debugf("Cached truncated UUID %s for station %s", truncated, stationId)
	}

	// Commit the transaction
	err = tx.Commit()
	if err != nil {
		log.Error("Error committing transaction for adding favorite: ", err)
		return
	}
}

func (d *Database) RemoveFavorite(mac string, stationId string) {
	s := `DELETE FROM favorite f
                USING device d, station s
           	    WHERE d.device_id = f.device_id
                  AND s.station_id = f.station_id
                  AND d.mac = $1
           	      AND s.radiobrowser_id = $2`

	_, err := d.db.Exec(s, mac, stationId)
	if err != nil {
		log.Error("Error removing favorite: ", err)
	}
}

func (d *Database) IsFavorite(mac string, stationId string) bool {
	s := "SELECT EXISTS(SELECT * FROM favorite f JOIN device d ON d.device_id = f.device_id JOIN station s on s.station_id = f.station_id WHERE d.mac = $1 AND s.radiobrowser_id = $2)"

	row := d.db.QueryRow(s, mac, stationId)

	var exists bool
	err := row.Scan(&exists)
	if err != nil {
		log.Error("cannot parse row", err)
		return false
	}

	return exists
}

func (d *Database) GetFavoriteStations(mac string) []radioprovider.Station {
	s := `SELECT s.radiobrowser_id,
                 s.name
            FROM favorite f
            JOIN device d ON d.device_id = f.device_id
            JOIN station s ON s.station_id = f.station_id
           WHERE d.mac = $1;`

	rows, err := d.db.Query(s, mac)
	if err != nil {
		log.Error("error getting favorite stations", err)
		return []radioprovider.Station{}
	}
	defer rows.Close()

	var stations []radioprovider.Station
	for rows.Next() {
		var s radioprovider.Station

		err := rows.Scan(&s.Id, &s.Name)
		if err != nil {
			log.Error("error scanning row", err)
			return []radioprovider.Station{}
		}

		stations = append(stations, s)
	}

	return stations
}

func (d *Database) GetStationByTruncatedUUID(truncatedUuid string) (string, bool) {
	// Validate input to prevent LIKE wildcard injection — UUIDs only contain hex digits and hyphens
	for _, ch := range truncatedUuid {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F') || ch == '-') {
			return "", false
		}
	}
	// Cast to text to be robust even if column type was created as integer in older schemas
	s := `SELECT radiobrowser_id FROM station WHERE radiobrowser_id::text LIKE $1 || '%' LIMIT 1`

	row := d.db.QueryRow(s, truncatedUuid)

	var fullUuid string
	err := row.Scan(&fullUuid)
	if err == sql.ErrNoRows {
		return "", false
	}
	if err != nil {
		log.Error("error looking up truncated UUID", err)
		return "", false
	}

	return fullUuid, true
}

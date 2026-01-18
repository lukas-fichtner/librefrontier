package main

import (
	"github.com/compujuckel/librefrontier/common"
	"github.com/compujuckel/librefrontier/common/radioprovider/radiobrowser"
	log "github.com/sirupsen/logrus"
	"go.uber.org/fx"
	"os"
)

func Startup(a *ApiServer) {

}

// provideTruncatedUUIDResolver adapts *common.Database to the interface expected by
// radiobrowser.NewRadioBrowserClient.
func provideTruncatedUUIDResolver(db *common.Database) interface{ GetStationByTruncatedUUID(string) (string, bool) } {
	return db
}

func main() {
	log.SetOutput(os.Stdout)
	log.SetLevel(log.DebugLevel)
	log.SetFormatter(&log.TextFormatter{
		ForceColors: true,
	})
	log.Info("Main Startup")

	app := fx.New(
		fx.Provide(
			common.NewEnvConfig,
			common.NewXmlBuilder,
			common.NewDatabase,
			// Adapter to satisfy radiobrowser client dependency on the truncated UUID resolver interface
			provideTruncatedUUIDResolver,
			radiobrowser.NewRadioBrowserClient,
			NewApiController,
		),
		fx.Invoke(Startup),
	)

	app.Run()
}

package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cyverse-de/go-mod/cfg"
	"github.com/cyverse-de/go-mod/logging"
	"github.com/cyverse-de/go-mod/otelutils"
	"github.com/cyverse-de/subscriptions/app"
	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/uptrace/opentelemetry-go-extra/otelsql"
	"github.com/uptrace/opentelemetry-go-extra/otelsqlx"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"

	_ "expvar"

	_ "github.com/doug-martin/goqu/v9/dialect/postgres"
	_ "github.com/lib/pq"
)

const serviceName = "subscriptions"

var log = logging.Log.WithFields(logrus.Fields{"package": "main"})

func main() {
	var (
		err    error
		config *koanf.Koanf
		dbconn *sqlx.DB

		configPath     = flag.String("config", cfg.DefaultConfigPath, "Path to the config file")
		dotEnvPath     = flag.String("dotenv-path", cfg.DefaultDotEnvPath, "Path to the dotenv file")
		envPrefix      = flag.String("env-prefix", "QMS_", "The prefix for environment variables")
		reportOverages = flag.Bool("report-overages", true, "Allows the overages feature to effectively be shut down")
		logLevel       = flag.String("log-level", "debug", "One of trace, debug, info, warn, error, fatal, or panic.")
		listenPort     = flag.Int("port", 60000, "The port the service listens on for requests")
	)

	flag.Parse()
	logging.SetupLogging(*logLevel)

	log := log.WithFields(logrus.Fields{"context": "main"})

	var tracerCtx, cancel = context.WithCancel(context.Background())
	defer cancel()
	shutdown := otelutils.TracerProviderFromEnv(tracerCtx, serviceName, func(e error) { log.Fatal(e) })
	defer shutdown()

	config, err = cfg.Init(&cfg.Settings{
		EnvPrefix:   *envPrefix,
		ConfigPath:  *configPath,
		DotEnvPath:  *dotEnvPath,
		StrictMerge: false,
		FileType:    cfg.YAML,
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Infof("Done reading config from %s", *configPath)

	dbURI := config.String("database.uri")
	if dbURI == "" {
		log.Fatal("database.uri must be set in the configuration file")
	}

	// Make sure the db.uri URL is parseable
	if _, err = url.Parse(dbURI); err != nil {
		log.Fatal(errors.Wrap(err, "Can't parse database.uri in the config file"))
	}

	userSuffix := strings.Trim(config.String("users.domain"), "@")
	if userSuffix == "" {
		log.Fatal("users.domain must be set in the configuration file")
	}

	log.Infof("username suffix is configured as %s", userSuffix)

	log.Infof("--report-overages is %t", *reportOverages)

	dbconn = otelsqlx.MustConnect("postgres", dbURI,
		otelsql.WithAttributes(semconv.DBSystemPostgreSQL))
	log.Info("done connecting to the database")
	dbconn.SetMaxOpenConns(10)
	dbconn.SetConnMaxIdleTime(time.Minute)

	a := app.New(dbconn, userSuffix, *reportOverages)

	// The QMS handlers strip the suffix with strings.TrimSuffix, so they need
	// the leading "@" that users.domain was trimmed of above. Passing the bare
	// domain would turn "user@example.org" into "user@" rather than "user".
	a.RegisterQMSAPI("@" + userSuffix)
	log.Info("registered the QMS /v1 API")

	srv := fmt.Sprintf(":%s", strconv.Itoa(*listenPort))
	log.Fatal(http.ListenAndServe(srv, a.Router))
}

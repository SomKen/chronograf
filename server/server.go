package server

import (
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/influxdata/chronograf"
	"github.com/influxdata/chronograf/bolt"
	"github.com/influxdata/chronograf/canned"
	"github.com/influxdata/chronograf/influx"
	"github.com/influxdata/chronograf/layouts"
	clog "github.com/influxdata/chronograf/log"
	"github.com/influxdata/chronograf/uuid"
	flags "github.com/jessevdk/go-flags"
	"github.com/tylerb/graceful"
)

var logger = clog.New()

// Server for the chronograf API
type Server struct {
	Host string `long:"host" description:"the IP to listen on" default:"localhost" env:"HOST"`
	Port int    `long:"port" description:"the port to listen on for insecure connections, defaults to a random value" default:"8888" env:"PORT"`

	TLSHost           string         `long:"tls-host" description:"the IP to listen on for tls, when not specified it's the same as --host" env:"TLS_HOST"`
	TLSPort           int            `long:"tls-port" description:"the port to listen on for secure connections, defaults to a random value" env:"TLS_PORT"`
	TLSCertificate    flags.Filename `long:"tls-certificate" description:"the certificate to use for secure connections" env:"TLS_CERTIFICATE"`
	TLSCertificateKey flags.Filename `long:"tls-key" description:"the private key to use for secure conections" env:"TLS_PRIVATE_KEY"`

	Develop            bool   `short:"d" long:"develop" description:"Run server in develop mode."`
	BoltPath           string `short:"b" long:"bolt-path" description:"Full path to boltDB file (/Users/somebody/chronograf.db)" env:"BOLT_PATH" default:"chronograf.db"`
	CannedPath         string `short:"c" long:"canned-path" description:"Path to directory of pre-canned application layouts" env:"CANNED_PATH" default:"canned"`
	TokenSecret        string `short:"t" long:"token-secret" description:"Secret to sign tokens" env:"TOKEN_SECRET"`
	GithubClientID     string `short:"i" long:"github-client-id" description:"Github Client ID for OAuth 2 support" env:"GH_CLIENT_ID"`
	GithubClientSecret string `short:"s" long:"github-client-secret" description:"Github Client Secret for OAuth 2 support" env:"GH_CLIENT_SECRET"`

	Listener net.Listener
	handler  http.Handler
}

func (s *Server) useAuth() bool {
	return s.TokenSecret != "" && s.GithubClientID != "" && s.GithubClientSecret != ""
}

// Serve starts and runs the chronograf server
func (s *Server) Serve() error {
	service := openService(s.BoltPath, s.CannedPath)
	s.handler = NewMux(MuxOpts{
		Develop:            s.Develop,
		TokenSecret:        s.TokenSecret,
		GithubClientID:     s.GithubClientID,
		GithubClientSecret: s.GithubClientSecret,
		Logger:             logger,
		UseAuth:            s.useAuth(),
	}, service)

	var err error
	s.Listener, err = net.Listen("tcp", net.JoinHostPort(s.Host, strconv.Itoa(s.Port)))
	if err != nil {
		logger.
			WithField("component", "server").
			Error(err)
		return err
	}

	httpServer := &graceful.Server{Server: new(http.Server)}
	httpServer.SetKeepAlivesEnabled(true)
	httpServer.TCPKeepAlive = 1 * time.Minute
	httpServer.Handler = s.handler

	logger.
		WithField("component", "server").
		Info("Serving chronograf at http://%s", s.Listener.Addr())

	if err := httpServer.Serve(s.Listener); err != nil {
		logger.
			WithField("component", "server").
			Error(err)
		return err
	}

	logger.
		WithField("component", "server").
		Info("Stopped serving chronograf at http://%s", s.Listener.Addr())

	return nil
}

func openService(boltPath, cannedPath string) Service {
	db := bolt.NewClient()
	db.Path = boltPath
	if err := db.Open(); err != nil {
		logger.
			WithField("component", "boltstore").
			Panic("Unable to open boltdb; is there a mrfusion already running?", err)
		panic(err)
	}

	apps := canned.NewApps(cannedPath, &uuid.V4{})
	// Acts as a front-end to both the bolt layouts and the filesystem layouts.
	layouts := &layouts.MultiLayoutStore{
		Stores: []chronograf.LayoutStore{
			db.LayoutStore,
			apps,
		},
	}

	return Service{
		ExplorationStore: db.ExplorationStore,
		SourcesStore:     db.SourcesStore,
		ServersStore:     db.ServersStore,
		TimeSeries:       &influx.Client{},
		LayoutStore:      layouts,
	}
}

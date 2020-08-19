package main

import (
	"context"
	"crypto/tls"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/limitz404/lokalise-listener/braze"
	"github.com/limitz404/lokalise-listener/github"
	"github.com/limitz404/lokalise-listener/logging"
	"github.com/limitz404/lokalise-listener/lokalise"
	"github.com/limitz404/lokalise-listener/utils"
)

const (
	httpAddress = ":https"
)

var (
	certificatePath = os.Getenv("TLS_CERTIFICATE_PATH")
	keyPath         = os.Getenv("TLS_PRIVATE_KEY_PATH")
)

func printRoutes(route *mux.Route, router *mux.Router, ancestors []*mux.Route) error {
	pathTemplate, _ := route.GetPathTemplate()
	pathRegexp, _ := route.GetPathRegexp()
	queriesTemplates, _ := route.GetQueriesTemplates()
	queriesRegexps, _ := route.GetQueriesRegexp()
	methods, _ := route.GetMethods()
	host, _ := route.GetHostTemplate()
	logging.Info().LogArgs("",
		logging.Args{
			"route":           pathTemplate,
			"path_regexp":     pathRegexp,
			"query_templates": strings.Join(queriesTemplates, ","),
			"query_regexps":   strings.Join(queriesRegexps, ","),
			"methods":         strings.Join(methods, ","),
			"host":            host,
		})
	return nil
}

func main() {
	verboseLogging := flag.Bool("verbose", false, "enable verbose logging")
	flag.Parse()
	utils.VerboseLogging = *verboseLogging

	go braze.StartStringsCacheEvictionLoop()

	router := mux.NewRouter()
	router.Use(utils.AddUniqueRequestID)
	router.Use(utils.LogRequest)
	static := router.PathPrefix("/static").Host("www.makeshift.dev")
	staticServer := http.FileServer(utils.NeuteredFileSystem{FS: http.Dir("./static")})
	static.Handler(http.StripPrefix("/static", staticServer)).Methods(http.MethodGet)

	lokaliseAPI := router.PathPrefix("/api/v1/lokalise").Host("www.makeshift.dev").Subrouter()
	lokaliseAPI.Handle("/order_complete", utils.ValidateAPIKey(http.HandlerFunc(lokalise.TaskCompletedHandler))).Methods(http.MethodPost)

	brazeAPI := router.PathPrefix("/api/v1/braze").Host("www.makeshift.dev").Subrouter()
	brazeAPI.HandleFunc("/parse_template", braze.ParseTemplateHandler).Methods(http.MethodPost)
	brazeAPI.Handle("/strings", utils.ValidateAPIKey(http.HandlerFunc(braze.GetStringsHandler))).Methods(http.MethodGet, http.MethodPost)

	githubAPI := router.PathPrefix("/api/v1/github").Host("www.makeshift.dev").Subrouter()
	githubAPI.HandleFunc("/ping", github.PingHandler).Methods(http.MethodPost)

	facepalmServer := http.FileServer(utils.NeuteredFileSystem{FS: http.Dir("./facepalm")})
	router.Host("www.facepalm.wtf").Handler(facepalmServer).Methods(http.MethodGet)
	router.Host("facepalm.wtf").Handler(facepalmServer).Methods(http.MethodGet)

	router.Walk(printRoutes)

	srv := &http.Server{
		Addr:         httpAddress,
		WriteTimeout: time.Second * 15,
		ReadTimeout:  time.Second * 15,
		IdleTimeout:  time.Second * 60,
		Handler:      router,
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler), 0),
		TLSConfig: &tls.Config{
			MinVersion:               tls.VersionTLS12,
			CurvePreferences:         []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
			PreferServerCipherSuites: true,
			CipherSuites: []uint16{
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
				tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_RSA_WITH_AES_256_CBC_SHA,
			},
		},
	}

	logging.Info().LogArgs("listening for http/https: {{.address}}", logging.Args{"address": httpAddress})
	go func() {
		if err := srv.ListenAndServeTLS(certificatePath, keyPath); err != nil {
			logging.Fatal().LogErr("failed to start http server", err)
		}
	}()

	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, os.Interrupt, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	for {
		sig := <-signalChannel
		if sig == syscall.SIGHUP {
			utils.VerboseLogging = !utils.VerboseLogging
			logging.Info().LogArgs("verbose logging set to {{.value}}",
				logging.Args{
					"value": logging.Bool(utils.VerboseLogging),
				})
		} else {
			break
		}
	}

	logging.Info().Log("Caught signal - handling graceful shutdown")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

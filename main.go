package main

import (
	"context"
	"crypto/tls"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/limitz404/lokalise-listener/braze"
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
	logging.Trace().Log("\n" +
		utils.PrettyJSON(logging.Args{
			"route":           pathTemplate,
			"path_regexp":     pathRegexp,
			"query_templates": strings.Join(queriesTemplates, ","),
			"query_regexps":   strings.Join(queriesRegexps, ","),
			"methods":         strings.Join(methods, ","),
		}))
	return nil
}

func main() {
	go braze.StartStringsCacheEvictionLoop()

	router := mux.NewRouter()
	router.Use(utils.LogRequest)
	router.Use(utils.NeuterRequest)
	static := router.PathPrefix("/static")
	static.Handler(http.StripPrefix("/static", http.FileServer(http.Dir("./static")))).Methods(http.MethodGet)

	api := router.PathPrefix("/api/v1").Subrouter()
	api.HandleFunc("/taskComplete", lokalise.TaskCompletedHandler).Methods(http.MethodPost)
	api.Handle("/lokalise/order_complete", utils.ValidateAPIKey(http.HandlerFunc(lokalise.TaskCompletedHandler))).Methods(http.MethodPost)
	api.HandleFunc("/braze/parse_template", braze.ParseTemplateHandler).Methods(http.MethodPost)
	api.Handle("/strings/braze", utils.ValidateAPIKey(http.HandlerFunc(braze.GetStringsHandler))).Methods(http.MethodGet, http.MethodPost)

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
	signal.Notify(signalChannel, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	<-signalChannel

	logging.Info().Log("Caught signal - handling graceful shutdown")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

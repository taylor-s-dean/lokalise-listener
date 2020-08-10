package main

import (
	"net/http"
	"os"
	"strings"

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
	api.HandleFunc("/braze/parse_template", braze.ParseTemplateHandler).Methods(http.MethodPost)
	api.HandleFunc("/strings/braze", braze.GetStringsHandler).Methods(http.MethodPost)

	router.Walk(printRoutes)

	logging.Info().LogArgs("listening for http/https: {{.address}}", logging.Args{"address": httpAddress})
	if err := http.ListenAndServeTLS(":https", certificatePath, keyPath, router); err != nil {
		logging.Fatal().LogErr("failed to start http server", err)
	}
}

package main

import (
	"net/http"
	"os"

	"github.com/gorilla/mux"
	"github.com/limitz404/lokalise-listener/logging"
)

const (
	httpAddress = ":https"
)

var (
	certificatePath = os.Getenv("TLS_CERTIFICATE_PATH")
	keyPath         = os.Getenv("TLS_PRIVATE_KEY_PATH")
)

func main() {
	router := mux.NewRouter()

	api := router.PathPrefix("/api/v1").Subrouter()
	api.HandleFunc("/taskComplete", wrapHandler(taskCompletedHandler)).Methods(http.MethodPost)
	api.HandleFunc("/braze/parse_template", wrapHandler(brazeParseTemplateHandler)).Methods(http.MethodPost)
	api.HandleFunc("/strings/braze", wrapHandler(getBrazeStringsHandler)).Methods(http.MethodGet)

	static := router.PathPrefix("/").Subrouter()
	static.HandleFunc("/braze/template_upload", wrapHandler(brazeTemplateUploadHandler)).Methods(http.MethodGet)

	logging.Info().LogArgs("listening for http/https: {{.address}}", logging.Args{"address": httpAddress})
	if err := http.ListenAndServeTLS(":https", certificatePath, keyPath, router); err != nil {
		logging.Fatal().LogErr("failed to start http server", err)
	}
}

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/limitz404/lokalise-listener/logging"
)

var (
	certificatePath       = os.Getenv("TLS_CERTIFICATE_PATH")
	keyPath               = os.Getenv("TLS_PRIVATE_KEY_PATH")
	lokaliseWebhookSecret = os.Getenv("LOKALISE_WEBHOOK_SECRET")
	readOnlyAPIToken      = os.Getenv("LOKALISE_READ_ONLY_API_TOKEN")
)

const (
	httpAddress                    = ":https"
	projectsURL                    = "https://api.lokalise.com/api2/projects"
	lokaliseWebhookSecretHeaderKey = "x-secret"
)

func main() {
	router := mux.NewRouter()
	api := router.PathPrefix("/api/v1").Subrouter()
	api.HandleFunc("/taskComplete", wrapHandler(taskComplete)).Methods(http.MethodPost)
	static := router.PathPrefix("/").Subrouter()
	static.HandleFunc("/test", wrapHandler(test)).Methods(http.MethodGet)

	logging.Info().LogArgs("listening for http/https: {{.address}}", logging.Args{"address": httpAddress})
	if err := http.ListenAndServeTLS(":https", certificatePath, keyPath, router); err != nil {
		logging.Fatal().LogErr("failed to start http server", err)
	}
}

type requestHandler func(writer http.ResponseWriter, request *http.Request)

func wrapHandler(handler requestHandler) requestHandler {
	return func(writer http.ResponseWriter, request *http.Request) {
		logIncomingRequest(request)
		handler(writer, request)
	}
}

func logResponse(response *http.Response) {
	responseDump, err := httputil.DumpResponse(response, false)
	if err != nil {
		logging.Error().LogErr("unable to dump http request", err)
		return
	}

	bodyDump, err := ioutil.ReadAll(response.Body)
	if err := response.Body.Close(); err != nil {
		logging.Error().LogErr("failed to close request body", err)
	}
	response.Body = ioutil.NopCloser(bytes.NewBuffer(bodyDump))

	if err != nil {
		logging.Error().LogErr("failed to read request body", err)
	} else if len(bodyDump) > 0 && strings.Contains(response.Header.Get("Content-Type"), "application/json") {
		bodyObj := map[string]interface{}{}
		if err := json.Unmarshal(bodyDump, &bodyObj); err != nil {
			logging.Error().LogErr("failed to unmarshal JSON", err)
		}

		bodyDump, err = json.MarshalIndent(bodyObj, "", "    ")
		if err != nil {
			logging.Error().LogErr("failed to marshal JSON", err)
		}
	}

	logBuilder := strings.Builder{}
	logBuilder.WriteRune('\n')
	logBuilder.Write(responseDump)
	logBuilder.WriteRune('\n')
	logBuilder.Write(bodyDump)
	logging.Debug().Log(logBuilder.String())
}

func logRequest(request *http.Request, requestDump []byte) {
	bodyDump, err := ioutil.ReadAll(request.Body)
	if err := request.Body.Close(); err != nil {
		logging.Error().LogErr("failed to close request body", err)
	}
	request.Body = ioutil.NopCloser(bytes.NewBuffer(bodyDump))

	if err != nil {
		logging.Error().LogErr("failed to read request body", err)
	} else if len(bodyDump) > 0 && strings.Contains(request.Header.Get("Content-Type"), "application/json") {
		bodyObj := map[string]interface{}{}
		if err := json.Unmarshal(bodyDump, &bodyObj); err != nil {
			logging.Error().LogErr("failed to unmarshal JSON", err)
		}

		bodyDump, err = json.MarshalIndent(bodyObj, "", "    ")
		if err != nil {
			logging.Error().LogErr("failed to marshal JSON", err)
		}
	}

	logBuilder := strings.Builder{}
	logBuilder.WriteRune('\n')
	logBuilder.Write(requestDump)
	logBuilder.WriteRune('\n')
	logBuilder.Write(bodyDump)
	logging.Debug().Log(logBuilder.String())
}

func logIncomingRequest(request *http.Request) {
	requestDump, err := httputil.DumpRequest(request, false)
	if err != nil {
		logging.Error().LogErr("unable to dump http request", err)
		return
	}
	logRequest(request, requestDump)
}

func logOutgoingRequest(request *http.Request) {
	requestDump, err := httputil.DumpRequestOut(request, false)
	if err != nil {
		logging.Error().LogErr("unable to dump http request", err)
		return
	}
	logRequest(request, requestDump)
}

func validateLokaliseWebhookSecret(request *http.Request, secret string) error {
	value := request.Header.Get(lokaliseWebhookSecretHeaderKey)
	if value != secret {
		return errors.New("unable to validate request")
	}

	return nil
}

func respondWithError(w http.ResponseWriter, code int, message string) {
	respondWithJSON(w, code, map[string]string{"error": message})
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, _ := json.Marshal(payload)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}

func test(writer http.ResponseWriter, request *http.Request) {
	writer.Write([]byte(`
		<html>
		<head>
		<title>Test Successful</title>
		<basefont size=4>
		</head>
		<h1>This is a test of a web server.</h1>
		</body>
		</html>
	`))
}

func taskComplete(writer http.ResponseWriter, request *http.Request) {
	if err := validateLokaliseWebhookSecret(request, lokaliseWebhookSecret); err != nil {
		respondWithError(writer, http.StatusForbidden, err.Error())
		logging.Error().LogErr("unable to validate webhook secret", err)
		return
	}

	jsonBody := struct {
		Project struct {
			ID string
		}
	}{}

	body, err := ioutil.ReadAll(request.Body)
	if err := request.Body.Close(); err != nil {
		logging.Error().LogErr("failed to close request body", err)
	}

	if err != nil {
		logging.Error().LogErr("failed to read request body", err)
		return
	}

	if err := json.Unmarshal(body, &jsonBody); err != nil {
		logging.Error().LogErr("failed to unmarshal JSON body", err)
		return
	}

	exportStrings(jsonBody.Project.ID)
}

func exportStrings(projectID string) {
	urlBuilder := strings.Builder{}
	urlBuilder.WriteString(projectsURL)
	urlBuilder.WriteRune('/')
	urlBuilder.WriteString(projectID)
	urlBuilder.WriteString("/files/download")

	data := map[string]interface{}{
		"format":   "strings",
		"triggers": []string{"github"},
	}

	dataBytes, err := json.Marshal(data)
	if err != nil {
		logging.Error().LogErr("unable to marshal JSON body", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	request, err := http.NewRequestWithContext(
		ctx,
		"POST",
		urlBuilder.String(),
		bytes.NewBuffer(dataBytes),
	)

	if err != nil {
		logging.Error().LogErr("unable to create http request", err)
		return
	}

	request.Header.Set("content-type", "application/json")
	request.Header.Set("x-api-token", readOnlyAPIToken)

	client := &http.Client{}
	response, err := client.Do(request)
	if err != nil {
		logging.Error().LogErr("error reading response", err)
		return
	}
	logResponse(response)
}

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/limitz404/lokalise-listener/logging"
)

const (
	httpAddress = ":https"

	lokaliseURL                    = "https://api.lokalise.com"
	lokaliseProjectsAPI            = "/api2/projects"
	lokaliseWebhookSecretHeaderKey = "x-secret"

	brazeURL             = "https://rest.iad-01.braze.com"
	brazeTemplateInfoAPI = "/templates/email/info"
	brazeStringRegexpStr = `{{[ \t]*strings\.(?P<key>.+?)[ \t]*\|[ \t]*default:[ \t]*(?:'|\")(?P<default>.+?)(?:'|\")[ \t]*}}`
)

var (
	certificatePath       = os.Getenv("TLS_CERTIFICATE_PATH")
	keyPath               = os.Getenv("TLS_PRIVATE_KEY_PATH")
	lokaliseWebhookSecret = os.Getenv("LOKALISE_WEBHOOK_SECRET")
	readOnlyAPIToken      = os.Getenv("LOKALISE_READ_ONLY_API_TOKEN")
	brazeTemplateAPIKey   = os.Getenv("BRAZE_TEMPLATE_API_KEY")

	brazeStringRegexp = regexp.MustCompile(brazeStringRegexpStr)
)

func main() {
	router := mux.NewRouter()

	api := router.PathPrefix("/api/v1").Subrouter()
	api.HandleFunc("/taskComplete", wrapHandler(taskCompletedHandler)).Methods(http.MethodPost)
	api.HandleFunc("/braze/parse_template", wrapHandler(brazeParseTemplate)).Methods(http.MethodPost)
	api.HandleFunc("/strings/braze", wrapHandler(getBrazeStrings)).Methods(http.MethodGet)

	static := router.PathPrefix("/").Subrouter()
	static.HandleFunc("/braze/template_upload", wrapHandler(brazeTemplateUploadHandler)).Methods(http.MethodGet)

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

func prettyJSONString(bodyJSON map[string]interface{}) string {
	bodyBytes, err := prettyJSON(bodyJSON)
	if err != nil {
		return ""
	}
	return string(bodyBytes)
}

func prettyJSON(bodyJSON map[string]interface{}) ([]byte, error) {
	return json.MarshalIndent(bodyJSON, "", "    ")
}

func logResponse(response *http.Response) {
	if response == nil {
		return
	}

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

		bodyDump, err = prettyJSON(bodyObj)
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

func getBodyBytes(body *io.ReadCloser) ([]byte, error) {
	if body == nil || *body == nil {
		return nil, errors.New("body is nil")
	}

	bodyDump, err := ioutil.ReadAll(*body)
	if err != nil {
		return nil, err
	}

	if err := (*body).Close(); err != nil {
		return nil, err
	}
	*body = ioutil.NopCloser(bytes.NewBuffer(bodyDump))

	return bodyDump, nil
}

func getJSONBody(body *io.ReadCloser) (map[string]interface{}, error) {
	if body == nil {
		return nil, errors.New("body is nil")
	}

	bodyDump, err := getBodyBytes(body)
	if err != nil {
		return nil, err
	}

	bodyJSON := map[string]interface{}{}
	if err := json.Unmarshal(bodyDump, &bodyJSON); err != nil {
		return nil, err
	}

	return bodyJSON, nil
}

func dumpBody(body *io.ReadCloser, contentType string) []byte {
	if body == nil {
		return []byte{}
	}

	if strings.Contains(contentType, "application/json") {
		bodyJSON, err := getJSONBody(body)
		if err != nil {
			logging.Error().LogErr("unable to get JSON body", err)
			return []byte{}
		}

		bodyDump, err := json.MarshalIndent(bodyJSON, "", "    ")
		if err != nil {
			logging.Error().LogErr("failed to marshal JSON", err)
			return []byte{}
		}

		return bodyDump
	}

	bodyDump, err := getBodyBytes(body)
	if err != nil {
		logging.Error().LogErr("unable to dump body", err)
		return []byte{}
	}

	return bodyDump
}

func logRequest(request *http.Request, requestDump []byte) {
	if request == nil {
		return
	}

	logBuilder := strings.Builder{}
	logBuilder.WriteRune('\n')
	logBuilder.Write(requestDump)
	logBuilder.WriteRune('\n')
	logBuilder.Write(dumpBody(&request.Body, request.Header.Get("Content-Type")))
	logging.Debug().Log(logBuilder.String())
}

func logIncomingRequest(request *http.Request) {
	if request == nil {
		return
	}

	requestDump, err := httputil.DumpRequest(request, false)
	if err != nil {
		logging.Error().LogErr("unable to dump http request", err)
		return
	}
	logRequest(request, requestDump)
}

func logOutgoingRequest(request *http.Request) {
	if request == nil {
		return
	}

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

func brazeParseTemplate(writer http.ResponseWriter, request *http.Request) {
	templateID := request.PostFormValue("template_id")
	if len(templateID) == 0 {
		logging.Error().Log("template_id is empty")
		return
	}

	logging.Debug().LogArgs("received parse template request", logging.Args{"template_id": templateID})
	getBrazeTemplateInfo(templateID)

	data := map[string]interface{}{
		"template_id": templateID,
	}

	dataBytes, err := json.Marshal(data)
	if err != nil {
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}

	writer.Write(dataBytes)
}

func brazeTemplateUploadHandler(writer http.ResponseWriter, request *http.Request) {
	writer.Write([]byte(`
		<!DOCTYPE html>
		<html lang="en">
		<head>
		<meta charset="utf-8">
        <title>Braze2Lokalise</title>
		<script src="https://code.jquery.com/jquery-1.12.4.min.js"></script>
		</head>
		<body>
			<h1>Braze2Lokalise Translation Uploader</h1>
			<p>
				Please enter the API Indentifier (found at the bottom of the template page on Braze) here:
			</p>
			<form id="upload" action="/api/v1/braze/parse_template" method="post">
				<input type="text" name="template_id"/>
				<input type="submit" value="Submit">
			</form>
			<script>
			$("#upload").submit(function(e) {
				e.preventDefault();
				var template_id = $(this).find("input[name='template_id']").val()
				$.post($(this).attr("action"), {template_id: template_id}).success(function(data) {
					var obj = $.parseJSON(data)
					alert(("Successfully uploaded strings from template_id: ").concat(obj.template_id));
				}).fail(function() {
					alert("Failed to upload strings from template");
				});
			});
			</script>
		</body>
        </html>
	`))
}

func taskCompletedHandler(writer http.ResponseWriter, request *http.Request) {
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

	createStringsPullRequest(jsonBody.Project.ID)
}

func getBrazeTemplateInfo(templateID string) (map[string]interface{}, error) {
	if len(templateID) == 0 {
		logging.Error().Log("received empty templateID")
		return nil, errors.New("received empty templateID")
	}

	urlValues := url.Values{}
	urlValues.Add("email_template_id", templateID)
	urlBuilder := strings.Builder{}
	urlBuilder.WriteString(brazeURL)
	urlBuilder.WriteString(brazeTemplateInfoAPI)
	urlBuilder.WriteRune('?')
	urlBuilder.WriteString(urlValues.Encode())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		urlBuilder.String(),
		nil,
	)

	if err != nil {
		logging.Error().LogErr("unable to create http request", err)
		return nil, err
	}

	request.Header.Add("Authorization", "Bearer "+brazeTemplateAPIKey)

	logOutgoingRequest(request)

	client := &http.Client{}
	response, err := client.Do(request)
	if err != nil {
		logging.Error().LogErr("error reading response", err)
		return nil, err
	}
	logResponse(response)

	bodyJSON, err := getJSONBody(&response.Body)
	if err != nil {
		logging.Error().LogErr("unable to parse JSON body", err)
		return nil, err
	}
	templateBody := bodyJSON["body"].(string)
	logging.Debug().Log(templateBody)
	extractBrazeStrings(templateBody)

	return nil, nil
}

func createStringsPullRequest(projectID string) {
	urlBuilder := strings.Builder{}
	urlBuilder.WriteString(lokaliseURL)
	urlBuilder.WriteString(lokaliseProjectsAPI)
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
		http.MethodPost,
		urlBuilder.String(),
		bytes.NewBuffer(dataBytes),
	)

	if err != nil {
		logging.Error().LogErr("unable to create http request", err)
		return
	}

	request.Header.Set("content-type", "application/json")
	request.Header.Set("x-api-token", readOnlyAPIToken)

	logOutgoingRequest(request)

	client := &http.Client{}
	response, err := client.Do(request)
	if err != nil {
		logging.Error().LogErr("error reading response", err)
		return
	}
	logResponse(response)
}

func getBrazeStrings(writer http.ResponseWriter, request *http.Request) {
	data := map[string]interface{}{
		"test_key": "test value",
	}

	dataBytes, err := json.Marshal(data)
	if err != nil {
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}

	writer.Write(dataBytes)
}

func extractBrazeStrings(template string) {
	strings := brazeStringRegexp.FindAllStringSubmatch(template, -1)
	logging.Debug().Log(fmt.Sprint(strings))
}

package utils

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/limitz404/lokalise-listener/logging"
)

// RequestHandler is a function that handles an HTTP request.
type RequestHandler func(writer http.ResponseWriter, request *http.Request)

// Neuter prevents the http.Handler from displaying the directory layout.
func Neuter(next http.Handler) http.Handler {
	return http.HandlerFunc(WrapHandler(func(writer http.ResponseWriter, request *http.Request) {
		if len(request.URL.Path) == 0 || strings.HasSuffix(request.URL.Path, "/") {
			http.NotFound(writer, request)
			return
		}

		next.ServeHTTP(writer, request)
	}))
}

// WrapHandler wraps an HTTP request handler by logging the request then
// calling the handler.
func WrapHandler(handler RequestHandler) RequestHandler {
	return func(writer http.ResponseWriter, request *http.Request) {
		start := time.Now()
		if err := logIncomingRequest(request); err != nil {
			logging.Error().LogErr("failed to log incoming request", err)
		}
		handler(writer, request)
		logging.Info().LogArgs("request handled in {{.duration}}", logging.Args{
			"duration": logging.Duration(time.Now().Sub(start)),
		})
	}
}

// PrettyJSONString formats and prints a map as JSON.
func PrettyJSONString(stringJSON map[string]string) string {
	stringBytes, err := prettyJSON(stringJSON)
	if err != nil {
		return ""
	}
	return string(stringBytes)
}

// PrettyJSONInterface formats and prints a map as JSON.
func PrettyJSONInterface(bodyJSON map[string]interface{}) string {
	bodyBytes, err := prettyJSON(bodyJSON)
	if err != nil {
		return ""
	}
	return string(bodyBytes)
}

func prettyJSON(bodyJSON interface{}) ([]byte, error) {
	bodyBytes, err := json.MarshalIndent(bodyJSON, "", "    ")
	return bodyBytes, WrapError(err)
}

// LogResponse formats an HTTP reponse and prints it.
func LogResponse(response *http.Response) error {
	if response == nil {
		return nil
	}

	responseDump, err := httputil.DumpResponse(response, false)
	if err != nil {
		return WrapError(err)
	}

	bodyDump, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return WrapError(err)
	}

	if err := response.Body.Close(); err != nil {
		return WrapError(err)
	}
	response.Body = ioutil.NopCloser(bytes.NewBuffer(bodyDump))

	if len(bodyDump) > 0 && strings.Contains(response.Header.Get("Content-Type"), "application/json") {
		bodyObj := map[string]interface{}{}
		if err := json.Unmarshal(bodyDump, &bodyObj); err != nil {
			return WrapError(err)
		}

		bodyDump, err = prettyJSON(bodyObj)
		if err != nil {
			return WrapError(err)
		}
	}

	logBuilder := strings.Builder{}
	logBuilder.WriteRune('\n')
	logBuilder.Write(responseDump)
	logBuilder.WriteRune('\n')
	logBuilder.Write(bodyDump)
	logging.Debug().Log(logBuilder.String())

	return nil
}

func getBodyBytes(body *io.ReadCloser) ([]byte, error) {
	if body == nil || *body == nil {
		return []byte{}, nil
	}

	bodyDump, err := ioutil.ReadAll(*body)
	if err != nil {
		return nil, WrapError(err)
	}

	if err := (*body).Close(); err != nil {
		return nil, WrapError(err)
	}
	*body = ioutil.NopCloser(bytes.NewBuffer(bodyDump))

	return bodyDump, nil
}

// GetJSONBody unmarshals a JSON formatted HTTP body without
// destroying the body itself.
func GetJSONBody(body *io.ReadCloser) (map[string]interface{}, error) {
	if body == nil {
		return map[string]interface{}{}, nil
	}

	bodyDump, err := getBodyBytes(body)
	if err != nil {
		return nil, WrapError(err)
	}

	bodyJSON := map[string]interface{}{}
	if len(bodyDump) > 0 {
		if err := json.Unmarshal(bodyDump, &bodyJSON); err != nil {
			return nil, WrapError(err)
		}
	}

	return bodyJSON, nil
}

func dumpBody(body *io.ReadCloser, contentType string) ([]byte, error) {
	if body == nil {
		return []byte{}, nil
	}

	if strings.Contains(contentType, "application/json") {
		bodyJSON, err := GetJSONBody(body)
		if err != nil {
			return nil, WrapError(err)
		}

		bodyDump, err := json.MarshalIndent(bodyJSON, "", "    ")
		if err != nil {
			return nil, WrapError(err)
		}

		return bodyDump, nil
	}

	bodyDump, err := getBodyBytes(body)
	if err != nil {
		return nil, WrapError(err)
	}

	return bodyDump, nil
}

func logRequest(request *http.Request, requestDump []byte) error {
	if request == nil {
		return nil
	}

	logBuilder := strings.Builder{}
	logBuilder.WriteRune('\n')
	logBuilder.Write(requestDump)

	body, err := dumpBody(&request.Body, request.Header.Get("Content-Type"))
	if err != nil {
		return WrapError(err)
	}

	logBuilder.WriteRune('\n')
	logBuilder.Write(body)
	logging.Debug().Log(logBuilder.String())

	return nil
}

func logIncomingRequest(request *http.Request) error {
	if request == nil {
		return nil
	}

	requestDump, err := httputil.DumpRequest(request, false)
	if err != nil {
		return WrapError(err)
	}

	if err := logRequest(request, requestDump); err != nil {
		return WrapError(err)
	}

	return nil
}

// LogOutgoingRequest formats and logs an outbound HTTP request.
func LogOutgoingRequest(request *http.Request) error {
	if request == nil {
		return nil
	}

	requestDump, err := httputil.DumpRequestOut(request, false)
	if err != nil {
		return WrapError(err)
	}

	if err := logRequest(request, requestDump); err != nil {
		return WrapError(err)
	}

	return nil
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) error {
	response, err := json.Marshal(payload)
	if err != nil {
		return WrapError(err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)

	return nil
}

// WrapError adds stack information to an error so its origin can be easily deduced.
func WrapError(err error) error {
	if err == nil {
		return nil
	}

	file, function, line := logging.GetStackInfo(1)

	errorString := strings.Builder{}
	errorString.WriteRune('(')
	errorString.WriteString(file)
	errorString.WriteString(", ")
	errorString.WriteString(function)
	errorString.WriteString(", ")
	errorString.WriteString(line)
	errorString.WriteRune(')')
	errorString.WriteString("->")
	errorString.WriteString(err.Error())

	return errors.New(errorString.String())
}

// FlattenPostForm converts from a struct of lists to a map[string]string
// with one value per key.
func FlattenPostForm(form url.Values) (map[string]string, error) {
	formBytes, err := json.Marshal(form)
	if err != nil {
		return nil, WrapError(err)
	}

	formMap := map[string]interface{}{}
	if err := json.Unmarshal(formBytes, &formMap); err != nil {
		return nil, WrapError(err)
	}

	stringMap := map[string]string{}
	for key, value := range formMap {
		valueSlice := value.([]interface{})
		if len(valueSlice) != 1 {
			return nil, WrapError(errors.New("malformed form data"))
		}
		stringMap[key] = valueSlice[0].(string)
	}

	return stringMap, nil
}

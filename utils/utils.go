package utils

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"strings"

	"github.com/limitz404/lokalise-listener/logging"
)

// RequestHandler is a function that handles an HTTP request.
type RequestHandler func(writer http.ResponseWriter, request *http.Request)

// WrapHandler wraps an HTTP request handler by logging the request then
// calling the handler.
func WrapHandler(handler RequestHandler) RequestHandler {
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

// LogResponse formats an HTTP reponse and prints it.
func LogResponse(response *http.Response) {
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
		return []byte{}, nil
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

// GetJSONBody unmarshals a JSON formatted HTTP body without
// destroying the body itself.
func GetJSONBody(body *io.ReadCloser) (map[string]interface{}, error) {
	if body == nil {
		return map[string]interface{}{}, nil
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

func dumpBody(body *io.ReadCloser, contentType string) ([]byte, error) {
	if body == nil {
		return []byte{}, nil
	}

	if strings.Contains(contentType, "application/json") {
		bodyJSON, err := GetJSONBody(body)
		if err != nil {
			return nil, err
		}

		bodyDump, err := json.MarshalIndent(bodyJSON, "", "    ")
		if err != nil {
			return nil, err
		}

		return bodyDump, nil
	}

	bodyDump, err := getBodyBytes(body)
	if err != nil {
		return nil, err
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
		return err
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
		return err
	}
	logRequest(request, requestDump)

	return err
}

// LogOutgoingRequest formats and logs an outbound HTTP request.
func LogOutgoingRequest(request *http.Request) error {
	if request == nil {
		return nil
	}

	requestDump, err := httputil.DumpRequestOut(request, false)
	if err != nil {
		return err
	}
	logRequest(request, requestDump)

	return nil
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) error {
	response, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)

	return nil
}

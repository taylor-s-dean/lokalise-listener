package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"strings"

	"github.com/limitz404/lokalise-listener/logging"
)

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

func respondWithError(w http.ResponseWriter, code int, message string) {
	respondWithJSON(w, code, map[string]string{"error": message})
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, _ := json.Marshal(payload)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}

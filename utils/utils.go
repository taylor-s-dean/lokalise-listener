package utils

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	crand "crypto/rand"

	"github.com/gorilla/handlers"
	"github.com/limitz404/lokalise-listener/logging"
)

const (
	// ContentTypeHeader is the string key for the content-type header
	ContentTypeHeader = "Content-Type"

	// UniqueRequestIDHeaderKey is used to track a request in the logs
	UniqueRequestIDHeaderKey = "X-Unique-Request-Id"
)

var (
	authenticationSecret = os.Getenv("API_AUTHENTICATION_SECRET")

	// VerboseLogging is a flag that enables and disables verbose logs.
	VerboseLogging = false
)

func init() {
	b := make([]byte, 8)
	_, err := crand.Read(b)
	if err != nil {
		panic("Failed to read bytes for seeding rand.")
	}
	rand.Seed(int64(binary.LittleEndian.Uint64(b)))
}

// CombinedLoggingWriter writes trace logs.
type CombinedLoggingWriter struct {
	uniqueID  string
	startTime time.Time
	io.Writer
}

func (w CombinedLoggingWriter) Write(p []byte) (n int, err error) {
	logBuilder := strings.Builder{}
	logBuilder.Write(p[:len(p)-1])
	logBuilder.WriteRune(' ')
	logBuilder.WriteString(w.uniqueID)
	logBuilder.WriteRune(' ')
	logBuilder.WriteString(time.Now().Sub(w.startTime).String())
	logging.Trace().Log(logBuilder.String())

	return len(p), nil
}

type loggingResponseWriter struct {
	status int
	body   []byte
	http.ResponseWriter
}

func (w *loggingResponseWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *loggingResponseWriter) Write(body []byte) (int, error) {
	w.body = body
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}

	return w.ResponseWriter.Write(body)
}

// NeuterRequest prevents the http.Handler from displaying the directory layout.
func NeuterRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if len(request.URL.Path) == 0 || strings.HasSuffix(request.URL.Path, "/") {
			logging.Warn().Log("request was neutered")
			http.NotFound(writer, request)
			return
		}
		writer.Header().Add("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		next.ServeHTTP(writer, request)
	})
}

// ValidateAPIKey returns a 404 if the API key cannot be validated.
func ValidateAPIKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Secret-Token") != authenticationSecret {
			logging.Warn().Log("request secret failed validation")
			http.NotFound(writer, request)
			return
		}
		next.ServeHTTP(writer, request)
	})
}

// AddUniqueRequestID adds a header containing a unique request identifier number.
func AddUniqueRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		uniqueID := strconv.FormatUint(rand.Uint64(), 36)
		request.Header.Add(UniqueRequestIDHeaderKey, uniqueID)
		writer.Header().Add(UniqueRequestIDHeaderKey, uniqueID)
		next.ServeHTTP(writer, request)
	})
}

// LogRequest wraps an HTTP handler by logging the request then serving the request.
func LogRequest(next http.Handler) http.Handler {
	combinedLoggingWriter := CombinedLoggingWriter{startTime: time.Now()}
	return handlers.CombinedLoggingHandler(&combinedLoggingWriter, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		combinedLoggingWriter.uniqueID = request.Header.Get(UniqueRequestIDHeaderKey)
		if VerboseLogging {
			if err := logIncomingRequest(request); err != nil {
				logging.Error().LogErr("failed to log incoming request", err)
			}
		}

		loggingRW := &loggingResponseWriter{ResponseWriter: writer}

		next.ServeHTTP(loggingRW, request)

		if VerboseLogging {
			response := &http.Response{
				StatusCode:    loggingRW.status,
				Status:        http.StatusText(loggingRW.status),
				Proto:         "HTTP/1.1",
				ProtoMajor:    1,
				ProtoMinor:    1,
				ContentLength: int64(len(loggingRW.body)),
				Body:          ioutil.NopCloser(bytes.NewBuffer(loggingRW.body)),
				Request:       request,
				Header:        loggingRW.Header().Clone(),
			}

			LogResponse(response)
		}
	}))
}

// PrettyJSON formats and prints an interface{} as JSON.
func PrettyJSON(bodyJSON interface{}) string {
	bodyBytes, err := json.MarshalIndent(bodyJSON, "", "    ")
	if err != nil {
		logging.Error().LogErr("failed to marshal JSON", err)
		return ""
	}
	return string(bodyBytes)
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

	var bodyStr string

	if len(bodyDump) > 0 && strings.Contains(response.Header.Get("Content-Type"), "application/json") {
		bodyObj := map[string]interface{}{}
		if err := json.Unmarshal(bodyDump, &bodyObj); err != nil {
			return WrapError(err)
		}

		bodyStr = PrettyJSON(bodyObj)
	}

	logBuilder := strings.Builder{}
	logBuilder.WriteRune('\n')
	logBuilder.Write(responseDump)
	if len(bodyStr) > 0 {
		logBuilder.WriteRune('\n')
		logBuilder.WriteString(bodyStr)
	}
	logging.Trace().Log(logBuilder.String())

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
	logging.Trace().Log(logBuilder.String())

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

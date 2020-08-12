package braze

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/limitz404/lokalise-listener/logging"
	"github.com/limitz404/lokalise-listener/utils"
)

const (
	brazeURL             = "https://rest.iad-01.braze.com"
	brazeTemplateInfoAPI = "/templates/email/info"
	brazeStringRegexpStr = `{{[ \t]*strings\.(?P<key>.+?)[ \t]*\|[ \t]*default:[ \t]*(?:'|\")(?P<default>.+?)(?:'|\")[ \t]*}}([ \t]*<!--[ \t]*context:[ \t]*"(?P<context>.+?)"[ \t]*-->)?`
)

var (
	brazeTemplateAPIKey = os.Getenv("BRAZE_TEMPLATE_API_KEY")

	brazeStringRegexp         = regexp.MustCompile(brazeStringRegexpStr)
	brazeTemplateStringsCache = stringsCache{}
	brazeStringStore          = stringsStore{}
)

type stringsCache struct {
	sync.Map
}

type stringsCacheValue struct {
	EvictionTime time.Time
	Data         []byte
}

type stringsStore struct {
	sync.Map
}

func (cache *stringsStore) Get(key string) (map[string]brazeString, bool) {
	value, ok := cache.Load(key)
	if ok {
		return value.(map[string]brazeString), ok
	}

	return nil, ok
}

func newStringsCacheValue(data []byte) *stringsCacheValue {
	newValue := stringsCacheValue{Data: data}
	newValue.Touch()
	return &newValue
}

func (value *stringsCacheValue) Touch() {
	value.EvictionTime = time.Now().Add(5 * time.Second)
}

func (value stringsCacheValue) GetData() []byte {
	return value.Data
}

func (value stringsCacheValue) IsExpired() bool {
	return time.Now().After(value.EvictionTime)
}

func (cache *stringsCache) getKey(data map[string]string) (string, error) {
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return "", utils.WrapError(err)
	}

	return string(dataBytes), nil
}

func (cache *stringsCache) Evict() {
	cache.Range(func(key, value interface{}) bool {
		if value.(*stringsCacheValue).IsExpired() {
			cache.Delete(key)
		}
		return true
	})
}

func (cache *stringsCache) Get(key string) (*stringsCacheValue, bool) {
	value, ok := cache.Load(key)
	if ok {
		valueCopy := *value.(*stringsCacheValue)
		return &valueCopy, ok
	}

	return nil, ok
}

// StartStringsCacheEvictionLoop runs an infinite for-loop that
// periodically evicts values from the template strings cache.
func StartStringsCacheEvictionLoop() {
	for {
		brazeTemplateStringsCache.Evict()
		time.Sleep(5 * time.Second)
	}
}

func (cache *stringsCache) Fetch(writer http.ResponseWriter, request *http.Request) {
	// ------------------------------------------------------------------------
	// Prototype configuration
	// ------------------------------------------------------------------------
	testTemplateEn := "Hi {{.name}},"
	testTemplateEs := "{{.name}}, Hola!"

	// ------------------------------------------------------------------------
	// Get parameters for this template
	// ------------------------------------------------------------------------
	if err := request.ParseForm(); err != nil {
		http.Error(writer, "failed to parse form", http.StatusInternalServerError)
		return
	}

	var queryParams url.Values

	switch request.Method {
	case http.MethodGet:
		var err error
		queryParams, err = url.ParseQuery(request.URL.RawQuery)
		if err != nil {
			http.Error(writer, "failed to parse query parameters", http.StatusInternalServerError)
			return
		}
	case http.MethodPost:
		queryParams = request.PostForm
	}

	formData, err := utils.FlattenPostForm(queryParams)
	if err != nil {
		http.Error(writer, "failed to flatten form data", http.StatusInternalServerError)
		return
	}

	// ------------------------------------------------------------------------
	// Generate cache key
	// ------------------------------------------------------------------------
	key, err := cache.getKey(formData)
	if err != nil {
		http.Error(writer, "failed to get cache key", http.StatusInternalServerError)
		return
	}

	// ------------------------------------------------------------------------
	// Check if key exists in cache
	// ------------------------------------------------------------------------

	value, ok := cache.Get(key)

	if ok {
		value.Touch()
		cache.Store(key, value)
		writer.Write(value.GetData())
		writer.Header().Add("X-From-Cache", "1")
		writer.Header().Add(utils.ContentTypeHeader, "application/json")
		return
	}

	// ------------------------------------------------------------------------
	// Determine the language and populate the template parameters
	// ------------------------------------------------------------------------
	testTemplate := testTemplateEs
	testParagraph := "La dirección de correo electrónico que utiliza para iniciar sesión en OkCupid acaba de cambiar. Si no realizó este cambio, infórmenos de inmediato."
	testSignIn := "Iniciar Sesión"
	testUnsubscribe := "Cancelar Suscripción"
	if formData["locale"] == "en" {
		testTemplate = testTemplateEn
		testParagraph = "The email address you use to sign in to OkCupid was just changed. If you didn’t make this change, please let us know immediately."
		testSignIn = "Sign In"
		testUnsubscribe = "Unsubscribe"
	}

	t, err := template.New("").Parse(testTemplate)
	if err != nil {
		http.Error(writer, "failed to parse template", http.StatusInternalServerError)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, formData); err != nil {
		http.Error(writer, "failed to execute template", http.StatusInternalServerError)
	}

	// ------------------------------------------------------------------------
	// Generate the response JSON
	// ------------------------------------------------------------------------
	data := map[string]interface{}{
		"header":      buf.String(),
		"paragraph":   testParagraph,
		"sign_in":     testSignIn,
		"unsubscribe": testUnsubscribe,
	}

	dataBytes, err := json.Marshal(data)
	if err != nil {
		http.Error(writer, "failed to marshal JSON", http.StatusInternalServerError)
		return
	}

	// ------------------------------------------------------------------------
	// Write the response
	// ------------------------------------------------------------------------

	cache.Store(key, newStringsCacheValue(dataBytes))
	writer.Write(dataBytes)
	writer.Header().Add(utils.ContentTypeHeader, "application/json")
}

type brazeString struct {
	Default string `json:"default"`
	Context string `json:"context"`
}

func extractBrazeStrings(template string) (map[string]brazeString, error) {
	matches := brazeStringRegexp.FindAllStringSubmatch(template, -1)
	if len(matches) == 0 {
		return map[string]brazeString{}, nil
	}

	// Braze strings are parsed into a [][]string.
	// Each match is parsed into []string of size 3. The data at
	// each index is as follows:
	// 1) Full match
	// 2) String Key
	// 3) String Value
	// 4) Context

	stringMap := make(map[string]brazeString, len(matches))
	for _, match := range matches {
		if len(match) != 5 {
			return nil, utils.WrapError(errors.New("failed to parse strings"))
		}

		defaultString := match[2]
		defaultString = strings.ReplaceAll(defaultString, "[[", "{{")
		defaultString = strings.ReplaceAll(defaultString, "]]", "}}")
		stringMap[match[1]] = brazeString{
			Default: defaultString,
			Context: match[4],
		}
	}

	return stringMap, nil
}

func getBrazeTemplateInfo(templateID string) (map[string]brazeString, error) {
	if len(templateID) == 0 {
		return nil, utils.WrapError(errors.New("received empty templateID"))
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
		return nil, utils.WrapError(err)
	}

	request.Header.Add("Authorization", "Bearer "+brazeTemplateAPIKey)

	if utils.VerboseLogging {
		utils.LogOutgoingRequest(request)
	}

	client := &http.Client{}
	response, err := client.Do(request)
	if err != nil {
		return nil, utils.WrapError(err)
	}

	bodyJSON, err := utils.GetJSONBody(&response.Body)
	if err != nil {
		return nil, utils.WrapError(err)
	}

	templateBody, ok := bodyJSON["body"]
	if !ok {
		logging.Debug().Log(utils.PrettyJSON(bodyJSON))
		message, ok := bodyJSON["message"]
		if !ok {
			message = "could not retrieve Braze template body"
		}

		return nil, utils.WrapError(errors.New(message.(string)))
	}

	extractedStrings, err := extractBrazeStrings(templateBody.(string))
	if err != nil {
		return nil, utils.WrapError(err)
	}

	brazeStringStore.Store(templateID, extractedStrings)

	return extractedStrings, nil
}

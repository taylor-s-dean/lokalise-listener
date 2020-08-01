package braze

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	brazeStringRegexpStr = `{{[ \t]*strings\.(?P<key>.+?)[ \t]*\|[ \t]*default:[ \t]*(?:'|\")(?P<default>.+?)(?:'|\")[ \t]*}}`
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
	evictionTime time.Time
	data         []byte
}

type stringsStore struct {
	sync.Map
}

func (cache *stringsStore) Get(key string) (map[string]string, bool) {
	value, ok := cache.Load(key)
	if ok {
		return value.(map[string]string), ok
	}

	return nil, ok
}

func newStringsCacheValue(data []byte) *stringsCacheValue {
	newValue := stringsCacheValue{data: data}
	newValue.Touch()
	return &newValue
}

func (value *stringsCacheValue) Touch() {
	value.evictionTime = time.Now().Add(5 * time.Second)
}

func (value stringsCacheValue) GetData() []byte {
	return value.data
}

func (value stringsCacheValue) IsExpired() bool {
	return time.Now().After(value.evictionTime)
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
			logging.Info().LogArgs("evicting {{.key}}", logging.Args{"key": key.(string)})
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

	formData, err := utils.FlattenPostForm(request.PostForm)
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
		logging.Debug().LogArgs("cache hit", logging.Args{"key": key})
		return
	}
	logging.Debug().LogArgs("cache miss", logging.Args{"key": key})

	// ------------------------------------------------------------------------
	// Determine the language and populate the template parameters
	// ------------------------------------------------------------------------
	testTemplate := testTemplateEs
	testParagraph := "La dirección de correo electrónico que utiliza para iniciar sesión en OkCupid acaba de cambiar. Si no realizó este cambio, infórmenos de inmediato."
	if formData["locale"] == "en" {
		testTemplate = testTemplateEn
		testParagraph = "The email address you use to sign in to OkCupid was just changed. If you didn’t make this change, please let us know immediately."
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
		"header":    buf.String(),
		"paragraph": testParagraph,
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
	value, ok = cache.Get(key)
	if ok {
		logging.Debug().LogArgs("eviction time", logging.Args{"evictionTime": value.evictionTime.String()})
	}
	writer.Write(dataBytes)
}

func extractBrazeStrings(template string) (map[string]string, error) {
	matches := brazeStringRegexp.FindAllStringSubmatch(template, -1)
	if len(matches) == 0 {
		return map[string]string{}, nil
	}

	logging.Debug().Log(fmt.Sprint(matches))
	// Braze strings are parsed into a [][]string.
	// Each match is parsed into []string of size 3. The data at
	// each index is as follows:
	// 1) Full match
	// 2) String Key
	// 3) String Value

	stringMap := make(map[string]string, len(matches))
	for _, match := range matches {
		if len(match) != 3 {
			return nil, utils.WrapError(errors.New("failed to parse strings"))
		}

		defaultString := match[2]
		defaultString = strings.ReplaceAll(defaultString, "[[", "{{")
		defaultString = strings.ReplaceAll(defaultString, "]]", "}}")
		stringMap[match[1]] = defaultString
	}

	logging.Debug().Log("\n" + utils.PrettyJSONString(stringMap))
	return stringMap, nil
}

func getBrazeTemplateInfo(templateID string) (map[string]interface{}, error) {
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

	utils.LogOutgoingRequest(request)

	client := &http.Client{}
	response, err := client.Do(request)
	if err != nil {
		return nil, utils.WrapError(err)
	}
	// utils.LogResponse(response)

	bodyJSON, err := utils.GetJSONBody(&response.Body)
	if err != nil {
		return nil, utils.WrapError(err)
	}
	templateBody := bodyJSON["body"].(string)

	extractedStrings, err := extractBrazeStrings(templateBody)
	if err != nil {
		return nil, utils.WrapError(err)
	}

	brazeStringStore.Store(templateID, extractedStrings)
	brazeStringStore.Range(func(key, value interface{}) bool {
		logging.Debug().Log(key.(string) + ": \n" + utils.PrettyJSONString(value.(map[string]string)))
		return true
	})
	value, ok := brazeStringStore.Get(templateID)
	if ok {
		logging.Debug().Log(utils.PrettyJSONString(value))
	}

	return nil, nil
}

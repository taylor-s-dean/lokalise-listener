package braze

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
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

	brazeStringRegexp = regexp.MustCompile(brazeStringRegexpStr)
)

func extractBrazeStrings(template string) (map[string]string, error) {
	strings := brazeStringRegexp.FindAllStringSubmatch(template, -1)
	if len(strings) == 0 {
		return map[string]string{}, nil
	}

	logging.Debug().Log(fmt.Sprint(strings))
	// Braze strings are parsed into a [][]string.
	// Each match is parsed into []string of size 3. The data at
	// each index is as follows:
	// 1) Full match
	// 2) String Key
	// 3) String Value

	stringMap := make(map[string]string, len(strings))
	for _, match := range strings {
		if len(match) != 3 {
			return nil, utils.WrapError(errors.New("failed to parse strings"))
		}

		stringMap[match[1]] = match[2]
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

	extractBrazeStrings(templateBody)

	return nil, nil
}

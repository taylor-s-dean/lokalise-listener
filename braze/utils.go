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

func extractBrazeStrings(template string) {
	strings := brazeStringRegexp.FindAllStringSubmatch(template, -1)
	logging.Debug().Log(fmt.Sprint(strings))
}

func getBrazeTemplateInfo(templateID string) (map[string]interface{}, error) {
	if len(templateID) == 0 {
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
		return nil, err
	}

	request.Header.Add("Authorization", "Bearer "+brazeTemplateAPIKey)

	utils.LogOutgoingRequest(request)

	client := &http.Client{}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	utils.LogResponse(response)

	bodyJSON, err := utils.GetJSONBody(&response.Body)
	if err != nil {
		return nil, err
	}
	templateBody := bodyJSON["body"].(string)
	logging.Debug().Log(templateBody)
	extractBrazeStrings(templateBody)

	return nil, nil
}

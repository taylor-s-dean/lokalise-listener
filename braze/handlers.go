package braze

import (
	"encoding/json"
	"net/http"

	"github.com/limitz404/lokalise-listener/logging"
	"github.com/limitz404/lokalise-listener/utils"
)

// ParseTemplateHandler receives the form values from a HTML form and
// parses out a Braze template ID. It then initiates the process of parsing
// L10N strings from the template and uploading those to Lokalise.
func ParseTemplateHandler(writer http.ResponseWriter, request *http.Request) {
	templateID := request.PostFormValue("template_id")
	if len(templateID) == 0 {
		logging.Error().Log("template_id is empty")
		return
	}

	templateStrings, err := getBrazeTemplateInfo(templateID)
	if err != nil {
		http.Error(writer, "unable to parse template", http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"template_id": templateID,
	}

	for key, value := range templateStrings {
		data[key] = value
	}

	dataBytes, err := json.Marshal(data)
	if err != nil {
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}

	writer.Write(dataBytes)
	writer.Header().Add(utils.ContentTypeHeader, "application/json")
}

// GetStringsHandler responds to a Braze connected content request to get
// localizeable strings for a given template.
func GetStringsHandler(writer http.ResponseWriter, request *http.Request) {
	brazeTemplateStringsCache.Fetch(writer, request)
}

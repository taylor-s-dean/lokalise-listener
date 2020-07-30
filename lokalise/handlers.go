package lokalise

import (
	"encoding/json"
	"io/ioutil"
	"net/http"

	"github.com/limitz404/lokalise-listener/logging"
)

// TaskCompletedHandler responds to an incoming webhook from Lokalise
// incdicating that translation task is complete and a pull request should
// be created in the corresponding GitHub repository.
func TaskCompletedHandler(writer http.ResponseWriter, request *http.Request) {
	if err := validateLokaliseWebhookSecret(request, lokaliseWebhookSecret); err != nil {
		http.Error(writer, err.Error(), http.StatusForbidden)
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

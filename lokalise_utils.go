package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/limitz404/lokalise-listener/logging"
)

const (
	lokaliseURL                    = "https://api.lokalise.com"
	lokaliseProjectsAPI            = "/api2/projects"
	lokaliseWebhookSecretHeaderKey = "x-secret"
)

var (
	lokaliseWebhookSecret = os.Getenv("LOKALISE_WEBHOOK_SECRET")
	readOnlyAPIToken      = os.Getenv("LOKALISE_READ_ONLY_API_TOKEN")
)

func validateLokaliseWebhookSecret(request *http.Request, secret string) error {
	value := request.Header.Get(lokaliseWebhookSecretHeaderKey)
	if value != secret {
		return errors.New("unable to validate request")
	}

	return nil
}

func createStringsPullRequest(projectID string) {
	urlBuilder := strings.Builder{}
	urlBuilder.WriteString(lokaliseURL)
	urlBuilder.WriteString(lokaliseProjectsAPI)
	urlBuilder.WriteRune('/')
	urlBuilder.WriteString(projectID)
	urlBuilder.WriteString("/files/download")

	data := map[string]interface{}{
		"format":   "strings",
		"triggers": []string{"github"},
	}

	dataBytes, err := json.Marshal(data)
	if err != nil {
		logging.Error().LogErr("unable to marshal JSON body", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		urlBuilder.String(),
		bytes.NewBuffer(dataBytes),
	)

	if err != nil {
		logging.Error().LogErr("unable to create http request", err)
		return
	}

	request.Header.Set("content-type", "application/json")
	request.Header.Set("x-api-token", readOnlyAPIToken)

	logOutgoingRequest(request)

	client := &http.Client{}
	response, err := client.Do(request)
	if err != nil {
		logging.Error().LogErr("error reading response", err)
		return
	}
	logResponse(response)
}

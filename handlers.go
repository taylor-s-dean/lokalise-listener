package main

import (
	"encoding/json"
	"io/ioutil"
	"net/http"

	"github.com/limitz404/lokalise-listener/logging"
)

func brazeParseTemplateHandler(writer http.ResponseWriter, request *http.Request) {
	templateID := request.PostFormValue("template_id")
	if len(templateID) == 0 {
		logging.Error().Log("template_id is empty")
		return
	}

	logging.Debug().LogArgs("received parse template request", logging.Args{"template_id": templateID})
	getBrazeTemplateInfo(templateID)

	data := map[string]interface{}{
		"template_id": templateID,
	}

	dataBytes, err := json.Marshal(data)
	if err != nil {
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}

	writer.Write(dataBytes)
}

func brazeTemplateUploadHandler(writer http.ResponseWriter, request *http.Request) {
	writer.Write([]byte(`
		<!DOCTYPE html>
		<html lang="en">
		<head>
		<meta charset="utf-8">
        <title>Braze2Lokalise</title>
		<script src="https://code.jquery.com/jquery-1.12.4.min.js"></script>
		</head>
		<body>
			<h1>Braze2Lokalise Translation Uploader</h1>
			<p>
				Please enter the API Indentifier (found at the bottom of the template page on Braze) here:
			</p>
			<form id="upload" action="/api/v1/braze/parse_template" method="post">
				<input type="text" name="template_id"/>
				<input type="submit" value="Submit">
			</form>
			<script>
			$("#upload").submit(function(e) {
				e.preventDefault();
				var template_id = $(this).find("input[name='template_id']").val()
				$.post($(this).attr("action"), {template_id: template_id}).success(function(data) {
					var obj = $.parseJSON(data)
					alert(("Successfully uploaded strings from template_id: ").concat(obj.template_id));
				}).fail(function() {
					alert("Failed to upload strings from template");
				});
			});
			</script>
		</body>
        </html>
	`))
}

func taskCompletedHandler(writer http.ResponseWriter, request *http.Request) {
	if err := validateLokaliseWebhookSecret(request, lokaliseWebhookSecret); err != nil {
		respondWithError(writer, http.StatusForbidden, err.Error())
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

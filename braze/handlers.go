package braze

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/limitz404/lokalise-listener/logging"
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

	logging.Debug().LogArgs("received parse template request", logging.Args{"template_id": templateID})
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
}

// GetStringsHandler responds to a Braze connected content request to get
// localizeable strings for a given template.
func GetStringsHandler(writer http.ResponseWriter, request *http.Request) {
	brazeTemplateStringsCache.Fetch(writer, request)
}

// TemplateUploadHandler displays a form to submit a Braze template ID to be parsed
// and uploaded to Lokalise.
func TemplateUploadHandler(writer http.ResponseWriter, request *http.Request) {
	logging.Debug().Log("here")
	pwd, err := os.Getwd()
	if err != nil {
		http.Error(writer, "failed to get current working directory", http.StatusInternalServerError)
		return
	}
	filePath := filepath.Join(pwd, "static/braze/template_upload.html")
	logging.Debug().Log(filePath)
	info, err := os.Stat(filepath.Join(pwd, "static/braze/template_upload.html"))
	if err != nil {
		logging.Error().LogErr("stat failed", err)
	}

	logging.Debug().Log(logging.JSON(info))

	http.ServeFile(writer, request, filePath)
	// writer.Write([]byte(`
	// 	<!DOCTYPE html>
	// 	<html lang="en">
	// 	<head>
	// 	<meta charset="utf-8">
	// <title>Braze2Lokalise</title>
	// 	<script src="https://code.jquery.com/jquery-1.12.4.min.js"></script>
	// 	</head>
	// 	<body>
	// 		<h1>Braze2Lokalise Translation Uploader</h1>
	// 		<p>
	// 			Please enter the API Indentifier (found at the bottom of the template page on Braze) here:
	// 		</p>
	// 		<form id="upload" action="/api/v1/braze/parse_template" method="post">
	// 			<input type="text" name="template_id"/>
	// 			<input type="submit" value="Submit">
	// 		</form>
	// 		<pre id="result">
	// 		</pre>
	// 		<script>
	// 		$("#upload").submit(function(e) {
	// 			e.preventDefault();
	// 			var template_id = $(this).find("input[name='template_id']").val()
	// 			$.post($(this).attr("action"), {template_id: template_id}).success(function(data) {
	// 				var obj = $.parseJSON(data)
	// 				$('#result').text(("Successfully uploaded strings from template:\n").concat(JSON.stringify(obj, null, 4)));
	// 			}).fail(function() {
	// 				alert("Failed to upload strings from template");
	// 			});
	// 		});
	// 		</script>
	// 	</body>
	// </html>
	// `))
}

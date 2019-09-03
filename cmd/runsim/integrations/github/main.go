package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/cosmos/tools/lib/runsimaws"
	"github.com/cosmos/tools/lib/runsimgh"
)

const (
	// security tokens
	ghAppTokenId  = "github-sim-app-key"
	circleTokenId = "circle_build_token_simulation"

	// Github app parameters
	startSimCmd      = "Start sim"
	ghCheckName      = "Long sim"
	ghConclusionFail = "failure" // The final conclusion of a failed github check

	// GitHub app installation details
	appIntegrationId  = "30867"
	appInstallationId = "997580"

	amiVersion = "master"

	// DynamoDB attribute and table names
	simStateTable = "SimulationState" // name of the dynamodb table where details from running simulations are stored
	primaryKey    = "IntegrationType" // primary partition key used by the sim state table

	awsRegion = "us-east-1"
)

type CircleApiPayload struct {
	Revision        string `json:"revision"`
	BuildParameters struct {
		CommitHash      string `json:"gaia-commit-hash"`
		IntegrationType string `json:"integration-type"`
	} `json:"parameters"`
}

type GithubEventPayload struct {
	Issue struct {
		Number int `json:"number"`
		Pr     struct {
			Url string `json:"url,omitempty"`
		} `json:"pull_request,omitempty"`
	} `json:"issue"`

	Comment struct {
		Body string `json:"body"`
	} `json:"comment"`

	Repo struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
}

func handler(request events.APIGatewayProxyRequest) (response events.APIGatewayProxyResponse, err error) {
	ddb := new(runsimaws.DdbTable)
	ddb.Config(awsRegion, primaryKey, simStateTable)

	github := new(runsimgh.Integration)
	// Try to retrieve sim state from database. Success indicates another sim is still running
	if _ = ddb.GetState("GitHub", github); github.IntegrationType != nil {
		// TODO: send this response as a PR comment or some other way to notify the user
		return buildProxyResponse(200, "INFO: another sim is already in progress"), err
	}

	// Same check for a slack simulation
	if _ = ddb.GetState("Slack", github); github.IntegrationType != nil {
		// TODO: send this response as a PR comment or some other way to notify the user
		return buildProxyResponse(200, "INFO: another sim is already in progress"), err
	}

	var ghEvent GithubEventPayload
	if err = json.Unmarshal([]byte(request.Body), &ghEvent); err != nil {
		log.Printf("INFO: github request: %+v", request.Body)
		return buildProxyResponse(500, "ERROR: unmarshal github request"), err
	}
	if ghEvent.Issue.Pr.Url == "" {
		return buildProxyResponse(200, fmt.Sprint("INFO: not a PR comment")), nil
	}
	if ghEvent.Comment.Body != startSimCmd {
		return buildProxyResponse(200, fmt.Sprintf("INFO: not a sim command")), nil
	}

	if err = github.ConfigFromScratch(awsRegion, ghAppTokenId, ghEvent.Repo.Owner.Login, ghEvent.Repo.Name,
		ghCheckName, appInstallationId, appIntegrationId, strconv.Itoa(ghEvent.Issue.Number)); err != nil {
		return buildProxyResponse(500, "ERROR: github.ConfigFromScratch"), err
	}

	if err = github.CreateNewCheckRun(); err != nil {
		return buildProxyResponse(500, "ERROR: github.CreateNewCheckRun"), err
	}

	ssm := new(runsimaws.Ssm)
	ssm.Config(awsRegion)
	circleToken, err := ssm.GetParameter(circleTokenId)
	if err != nil {
		response = buildProxyResponse(500, "ERROR: ssm.GetParameter")
		return
	}

	payload := new(CircleApiPayload)
	payload.Revision = amiVersion
	payload.BuildParameters.CommitHash = github.PR.Head.GetSHA()
	payload.BuildParameters.IntegrationType = "github"

	if err = triggerCircleciJob(circleToken, *payload); err != nil {
		ghErr := github.ConcludeCheckRun(aws.String("Failed to trigger CircleCI build job"), aws.String(ghConclusionFail))
		if ghErr != nil {
			log.Printf("ERROR: github.ConcludeCheckRun: %v", err)
		}
		return buildProxyResponse(500, "ERROR: triggerCircleciJob"), err
	}

	return buildProxyResponse(200, fmt.Sprint("INFO: Init attempt finished")), err
}

func triggerCircleciJob(circleToken string, payload CircleApiPayload) (err error) {
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return
	}

	var request *http.Request
	url := fmt.Sprintf("https://circleci.com/api/v2/project/gh/tendermint/images/pipeline?circle-token=%s", circleToken)
	if request, err = http.NewRequest("POST", url, bytes.NewBuffer(jsonPayload)); err != nil {
		return
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "*/*") // without this header, CircleCI doesn't return valid JSON...

	var httpClient = &http.Client{Timeout: 2 * time.Second}
	_, err = httpClient.Do(request)

	return
}

func buildProxyResponse(responseCode int, message string) events.APIGatewayProxyResponse {
	log.Print(message)
	return events.APIGatewayProxyResponse{
		StatusCode: responseCode,
		Body:       message,
	}
}

func main() {
	lambda.Start(handler)
}

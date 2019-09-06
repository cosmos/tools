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
	"github.com/cosmos/tools/lib/common"
	"github.com/cosmos/tools/lib/runsimaws"
	"github.com/cosmos/tools/lib/runsimgh"
)

const (
	// Security token IDs
	ssmGhAppTokenId  = "github-sim-app-key"
	ssmCircleTokenId = "circle-token-sim"

	// Github app parameters
	startSimCmd      = "Start sim"
	startSimCmdDev	 = "Start sim dev"
	ghCheckName      = "Long sim"
	appIntegrationId  = "30867"
	appInstallationId = "997580"

	// The value to use in the conclusion field of a github check in case of failure.
	ghConclusionFail = "failure"

	// DynamoDB attribute and table names
	awsRegion = "us-east-1"
	simStateTable = "SimulationState" // name of the dynamodb table where details from running simulations are stored
	primaryKey    = "IntegrationType" // primary partition key used by the sim state table
)

func handler(request events.APIGatewayProxyRequest) (response events.APIGatewayProxyResponse, err error) {
	ddb := new(runsimaws.DdbTable)
	ddb.Config(awsRegion, primaryKey, simStateTable)

	github := new(runsimgh.Integration)
	// Try to retrieve sim state from database. Success indicates another sim is still running
	if _ = ddb.GetState("GitHub", github); github.IntegrationType != nil {
		// TODO: send this response as a PR comment or some other way to notify the user
		return buildProxyResponse(200, "INFO: another sim is already in progress"), err
	}

	var ghEvent common.GithubEventPayload
	if err = json.Unmarshal([]byte(request.Body), &ghEvent); err != nil {
		log.Printf("INFO: github request: %+v", request.Body)
		return buildProxyResponse(500, "ERROR: unmarshal github request"), err
	}
	if ghEvent.Issue.Pr.Url == "" {
		return buildProxyResponse(200, fmt.Sprint("INFO: not a PR comment")), nil
	}

	var amiVersion string
	switch ghEvent.Comment.Body {
	case startSimCmd:
		amiVersion = "ami-gaia-sim"
	case startSimCmdDev:
		amiVersion = "master"
	default:
		return buildProxyResponse(200, fmt.Sprintf("INFO: not a sim command")), nil
	}

	err = github.ConfigFromScratch(awsRegion, ssmGhAppTokenId, ghEvent.Repo.Owner.Login, ghEvent.Repo.Name,
		ghCheckName, appInstallationId, appIntegrationId, strconv.Itoa(ghEvent.Issue.Number))
	if err != nil {
		cleanup(github)
		return buildProxyResponse(500, "ERROR: github.ConfigFromScratch"), err
	}

	if err = github.CreateNewCheckRun(); err != nil {
		cleanup(github)
		return buildProxyResponse(500, "ERROR: github.CreateNewCheckRun"), err
	}

	ssm := new(runsimaws.Ssm)
	ssm.Config(awsRegion)
	circleToken, err := ssm.GetParameter(ssmCircleTokenId)
	if err != nil {
		cleanup(github)
		response = buildProxyResponse(500, "ERROR: ssm.GetParameter")
		return
	}

	payload := new(common.CircleApiPayload)
	payload.Revision = amiVersion
	payload.BuildParameters.CommitHash = github.PR.Head.GetSHA()
	payload.BuildParameters.IntegrationType = "github"

	if err = triggerCircleciJob(circleToken, *payload); err != nil {
		ghErr := github.ConcludeCheckRun(aws.String("Failed to trigger CircleCI build job"), aws.String(ghConclusionFail))
		if ghErr != nil {
			log.Printf("ERROR: github.ConcludeCheckRun: %v", err)
		}
		cleanup(github)
		return buildProxyResponse(500, "ERROR: triggerCircleciJob"), err
	}

	msg := fmt.Sprintf("Image build in progress. [CircleCI](https://circleci.com/gh/tendermint/images/tree/%s)",
		amiVersion)
	err = github.UpdateCheckRunStatus(github.ActiveCheckRun.Status, &msg)

	return buildProxyResponse(200, fmt.Sprint("INFO: Init attempt finished")), err
}

func triggerCircleciJob(circleToken string, payload common.CircleApiPayload) (err error) {
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return
	}

	var request *http.Request
	url := fmt.Sprintf("https://circleci.com/api/v2/project/gh/tendermint/images/pipeline?circle-token=%s",
		circleToken)
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

// Function used if the program crashes out. Attempts to remove the state information from dynamoDB
func cleanup(github *runsimgh.Integration) {
	if err := github.DeleteState(); err != nil {
		log.Println(err)
	}
}

func main() {
	lambda.Start(handler)
}

package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/cosmos/tools/lib/common"
	"github.com/cosmos/tools/lib/runsimaws"
	"github.com/cosmos/tools/lib/runsimslack"
)

const (
	// Security token IDs
	ssmSlackSecretId   = "slack-cmd-secret"
	ssmCircleTokenId   = "circle-token-sim"
	ssmSlackChannelId  = "slack-channel-id"
	ssmSlackAppTokenId = "slack-app-key"

	slashCmd = "/sim_start"
	slashCmdDev = "/dev_sim_start"

	// DynamoDB attribute and table names
	awsRegion = "us-east-1"
	simStateTable = "SimulationState" // name of the dynamodb table where details from running simulations are stored
	primaryKey    = "IntegrationType" // primary partition key used by the sim state table
)

func parseSlackRequest(slashCmdPayload string) (payload common.CircleApiPayload, respUrl string, err error) {
	reFields := regexp.MustCompile(`^token.*?&command=(.*?)&text=(.*?)&response_url=(.*?)&`)
	reBlocks := regexp.MustCompile(`^[1-9][0-9]{0,3}$`)

	matches := reFields.FindStringSubmatch(slashCmdPayload)
	for _, match := range matches[1:] {
		if match, err = url.PathUnescape(match); err != nil {
			return
		}

		switch match {
		case "":
			return
		case slashCmd:
			payload.Revision = "ami-gaia-sim"
			continue
		case slashCmdDev:
			payload.Revision = "master"
			continue
		}

		// The second part of the command can contain absolutely anything that the user might decide to type.
		// It is common to use this text parameter to provide extra context for the command.
		simParams := strings.Split(match, "+")
		log.Printf("INFO: slash command parameters: %v", simParams)
		if len(simParams) > 1 {
			for _, param := range simParams {
				switch param {
				case reBlocks.FindString(param):
					payload.BuildParameters.Blocks = param
				case "yes":
					payload.BuildParameters.Genesis = "true"
				// Without this case the branch will be set to "no"
				// Temporary solution until we figure out how to handle custom genesis better
				// TODO: Figure out a better way to handle genesis file
				case "no":
					payload.BuildParameters.Genesis = "false"
				default:
					payload.BuildParameters.CommitHash = param
				}
			}
			continue
		}
		payload.BuildParameters.Integration = "slack"
		respUrl = match
	}
	return
}

func verifySlackRequest(slackSig, slackTimestamp, slackSecret, requestBody string) (err error) {
	intTimestamp, err := strconv.ParseInt(slackTimestamp, 10, 64)
	if err != nil {
		return
	}

	if math.Abs(time.Since(time.Unix(intTimestamp, 0)).Seconds()) > 120 {
		// The request timestamp is more than two minutes from local time.
		// It could be a replay attack, so let's ignore it.
		return errors.New("timestamp check failed")
	}

	requestHash := hmac.New(sha256.New, []byte(slackSecret))
	if _, err = requestHash.Write([]byte(fmt.Sprintf("v0:%s:%s", slackTimestamp, requestBody))); err != nil {
		return
	}
	if hmac.Equal([]byte(slackSig), []byte("v0="+hex.EncodeToString(requestHash.Sum(nil)))) {
		return
	}
	return errors.New("slack request verification failed")
}

func triggerCircleciJob(circleToken string, payload common.CircleApiPayload) (err error) {
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return
	}

	var request *http.Request
	circleUrl := fmt.Sprintf("https://circleci.com/api/v2/project/gh/tendermint/images/pipeline?circle-token=%s", circleToken)
	if request, err = http.NewRequest("POST", circleUrl, bytes.NewBuffer(jsonPayload)); err != nil {
		return
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "*/*") // without this header, CircleCI doesn't return valid JSON...

	var client = &http.Client{Timeout: 2 * time.Second}
	_, err = client.Do(request)
	return
}

func handler(request events.APIGatewayProxyRequest) (response events.APIGatewayProxyResponse, err error) {
	ssm := new(runsimaws.Ssm)
	ssm.Config(awsRegion)

	// Response code always has to be 200. https://api.slack.com/slash-commands#responding_to_commands
	response.StatusCode = 200

	circlePayload, respUrl, err := parseSlackRequest(request.Body)
	if err != nil {
		response.Body = fmt.Sprintf("ERROR: parseSlackRequest: %v", err)
		return
	}
	if circlePayload.Revision == "" {
		response.Body = "ERROR: slash command is missing parameters"
		return
	}

	// Need an immediate response to slack to avoid the command displaying a timeout error
	slack := new(runsimslack.Integration)
	err = slack.PushSlackCmdReply("Warming up!", respUrl)
	if err != nil {
		response.Body = fmt.Sprintf("ERROR: pushing slack message: %v", err)
		return
	}

	// Try to retrieve sim state from database. Success indicates another sim is still running
	ddb := new(runsimaws.DdbTable)
	ddb.Config(awsRegion, primaryKey, simStateTable)
	if _ = ddb.GetState("Slack", slack); slack.IntegrationType != nil {
		response.Body = "INFO: another simulation is in progress"
		return
	}

	slackSecret, err := ssm.GetParameter(ssmSlackSecretId)
	if err != nil {
		return
	}
	if err = verifySlackRequest(request.Headers["X-Slack-Signature"], request.Headers["X-Slack-Request-Timestamp"],
		slackSecret, request.Body); err != nil {
		response.Body = fmt.Sprintf("ERROR: verifySlackRequest: %v", err)
		return
	}

	circleToken, err := ssm.GetParameter(ssmCircleTokenId)
	if err != nil {
		return
	}
	if err = triggerCircleciJob(circleToken, circlePayload); err != nil {
		response.Body = fmt.Sprintf("ERROR: triggerCircleciJob: %v", err)
		return
	}

	channelId, err := ssm.GetParameter(ssmSlackChannelId)
	if err != nil {
		return
	}

	if err = slack.ConfigFromScratch(awsRegion, channelId, ssmSlackAppTokenId); err != nil {
		response.Body = fmt.Sprintf("ERROR: slack.ConfigFromScratch: %v", err)
		return
	}

	message := fmt.Sprintf("Simulation has started! <https://circleci.com/gh/tendermint/images/tree/%s|CircleCI>", circlePayload.Revision)
	if err = slack.PostMessage(message); err != nil {
		response.Body = fmt.Sprintf("ERROR: slack.PostMessage: %v", err)
		return
	}

	if err = ddb.PutState(slack); err != nil {
		response.Body = fmt.Sprintf("ERROR: ddb.PutState: %v", err)
		return
	}

	response.Body = "Init attempt complete."
	return
}

func main() {
	lambda.Start(handler)
}

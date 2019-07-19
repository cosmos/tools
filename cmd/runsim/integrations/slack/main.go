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
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ssm"
)

const paramNameSlackSecret = "slack_signing_secret"
const paramNameCircleToken = "circle_build_token_simulation"

var (
	err      error
	response *http.Response
	request  *http.Request
)

type CircleStatusCheckResp struct {
	Status    string `json:"status"`
	Lifecycle string `json:"lifecycle"`
}

type CircleJobTriggerResp struct {
	Status   string `json:"status"`
	Body     string `json:"body"`
	BuildURL string `json:"build_url"`
}

type CircleApiPayload struct {
	Revision        *string `json:"revision"`
	BuildParameters struct {
		CommitHash       string `json:"GAIA_COMMIT_HASH"`
		Blocks           string `json:"BLOCKS"`
		SlackResponseUrl string `json:"SLACK_RESP_URL"`
		Genesis          string `json:"GENESIS"`
	} `json:"build_parameters"`
}

type SlackPayload struct {
	Text string `json:"text"`
}

func pushSlackReply(message string, responseUrl string) error {
	var slackPayload []byte
	if slackPayload, err = json.Marshal(SlackPayload{
		Text: message,
	}); err != nil {
		return err
	}
	if request, err = http.NewRequest("POST", responseUrl, bytes.NewBuffer(slackPayload)); err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json;charset=UTF-8")
	httpClient := &http.Client{Timeout: 10 * time.Second}
	if response, err = httpClient.Do(request); err != nil {
		return err
	}
	defer response.Body.Close()
	return nil
}

func getSsmParameter(parameterName string) (string, error) {
	var getParamOutput *ssm.GetParameterOutput
	sessionSSM := ssm.New(session.Must(session.NewSession()))
	if getParamOutput, err = sessionSSM.GetParameter(&ssm.GetParameterInput{
		Name:           &parameterName,
		WithDecryption: aws.Bool(true),
	}); err != nil {
		return *getParamOutput.Parameter.Value, err
	}
	return *getParamOutput.Parameter.Value, nil
}

func processSlackRequest(slashCmdPayload string) (CircleApiPayload, error) {
	circleApiPayload := new(CircleApiPayload)
	reFields := regexp.MustCompile(`^token.*?&command=(.*?)&text=(.*?)&response_url=(.*?)&`)
	reBlocks := regexp.MustCompile(`^[1-9][0-9]{0,3}$`)

	matches := reFields.FindStringSubmatch(slashCmdPayload)
	for _, match := range matches[1:] {
		if match, err = url.PathUnescape(match); err != nil {
			return *circleApiPayload, err
		}
		if match == "" {
			return *circleApiPayload, nil
		}
		// First part of the slash command is the command name
		if match == "/sim_start" {
			circleApiPayload.Revision = aws.String("ami-gaia-sim")
			continue
		} else if match == "/dev-sim_start" {
			circleApiPayload.Revision = aws.String("master")
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
					circleApiPayload.BuildParameters.Blocks = param
				case "yes":
					circleApiPayload.BuildParameters.Genesis = "true"
				// Without this case the branch will be set to "no"
				// Temporary solution until we figure out how to handle custom genesis better
				// TODO: Figure out a better way to handle genesis file
				case "no":
					circleApiPayload.BuildParameters.Genesis = "false"
				default:
					circleApiPayload.BuildParameters.CommitHash = param
				}
			}
			continue
		}
		circleApiPayload.BuildParameters.SlackResponseUrl = match
	}
	return *circleApiPayload, nil
}

func verifySlackRequest(slackSig, slackTimestamp, requestBody string) error {
	var slackSecret string
	var intTimestamp int64

	if intTimestamp, err = strconv.ParseInt(slackTimestamp, 10, 64); err != nil {
		return err
	}
	if slackSecret, err = getSsmParameter(paramNameSlackSecret); err != nil {
		return err
	}
	if math.Abs(time.Since(time.Unix(intTimestamp, 0)).Seconds()) > 120 {
		// The request timestamp is more than two minutes from local time.
		// It could be a replay attack, so let's ignore it.
		return errors.New("timestamp check failed")
	}

	requestHash := hmac.New(sha256.New, []byte(slackSecret))
	if _, err = requestHash.Write([]byte("v0:" + slackTimestamp + ":" + requestBody)); err != nil {
		return err
	}
	if hmac.Equal([]byte(slackSig), []byte("v0="+hex.EncodeToString(requestHash.Sum(nil)))) {
		return nil
	}
	return errors.New("failed")
}

func circleciStatusCheck() error {
	var data []CircleStatusCheckResp

	circleUrl := "https://circleci.com/api/v1.1/project/github/tendermint/images?limit=1&shallow=true"
	if request, err = http.NewRequest("GET", circleUrl, nil); err != nil {
		return err
	}
	// Without this header, Circle doesn't return valid JSON...
	request.Header.Set("Accept", "*/*")

	httpClient := &http.Client{Timeout: 10 * time.Second}
	if response, err = httpClient.Do(request); err != nil {
		return err
	}
	defer response.Body.Close()

	if err = json.NewDecoder(response.Body).Decode(&data); err != nil {
		return err
	}
	if len(data) == 0 {
		return errors.New("ERROR: could not check status of simulation Circleci job")
	} else if data[0].Lifecycle != "finished" {
		return errors.New("ERROR: another simulation is already in progress")
	}
	return nil
}

func triggerCircleciJob(payload CircleApiPayload) error {
	var (
		circleBuildToken string
		jsonPayload      []byte
		request          *http.Request
		response         *http.Response
		err              error
		data             CircleJobTriggerResp
	)

	if circleBuildToken, err = getSsmParameter(paramNameCircleToken); err != nil {
		return err
	}
	circleJobUrl := fmt.Sprintf("https://circleci.com/api/v1.1/project/github/tendermint/images/tree/%s?circle-token=%s",
		*payload.Revision, circleBuildToken)

	if jsonPayload, err = json.Marshal(payload); err != nil {
		return err
	}
	log.Printf("INFO: circleci payload: %v", jsonPayload)
	if request, err = http.NewRequest("POST", circleJobUrl, bytes.NewBuffer(jsonPayload)); err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "*/*")

	client := &http.Client{Timeout: 2 * time.Second}
	if response, err = client.Do(request); err != nil {
		return err
	}
	defer response.Body.Close()
	if err = json.NewDecoder(response.Body).Decode(&data); err != nil {
		return err
	}
	if err = pushSlackReply(fmt.Sprintf("Follow progress notifications in #bot-simulation. Image build in progress: %s",
		data.BuildURL), payload.BuildParameters.SlackResponseUrl); err != nil {
		return err
	}
	return nil
}

func handler(request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	var circlePayload CircleApiPayload
	gatewayResponse := new(events.APIGatewayProxyResponse)
	gatewayResponse.StatusCode = 200

	if circlePayload, err = processSlackRequest(request.Body); err != nil {
		gatewayResponse.Body = fmt.Sprintf("ERROR: parsing slack command payload: %s", err.Error())
		return *gatewayResponse, nil
	}
	if circlePayload.Revision == nil {
		gatewayResponse.Body = "ERROR: slash command is missing parameters"
		return *gatewayResponse, nil
	}
	// Need an immediate response to slack to avoid the command displaying a timeout error
	if err = pushSlackReply("Warming up!", circlePayload.BuildParameters.SlackResponseUrl); err != nil {
		gatewayResponse.Body = fmt.Sprintf("ERROR: pushing slack message: %s", err.Error())
		return *gatewayResponse, nil
	}
	if err = verifySlackRequest(request.Headers["X-Slack-Signature"], request.Headers["X-Slack-Request-Timestamp"], request.Body); err != nil {
		gatewayResponse.Body = fmt.Sprintf("ERROR: verifying slack request: %s", err.Error())
		return *gatewayResponse, nil
	}
	if err = circleciStatusCheck(); err != nil {
		gatewayResponse.Body = fmt.Sprintf("ERROR: checking Circle job status: %s", err.Error())
		return *gatewayResponse, nil
	}
	if err = triggerCircleciJob(circlePayload); err != nil {
		gatewayResponse.Body = fmt.Sprintf("ERROR: triggering Circle job: %s", err.Error())
		return *gatewayResponse, nil
	}
	gatewayResponse.Body = "Init attempt complete."
	return *gatewayResponse, nil
}

func main() {
	lambda.Start(handler)
}

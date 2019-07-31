package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	ddb "github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/ssm"
	ghapp "github.com/bradleyfalzon/ghinstallation"
	gh "github.com/google/go-github/v27/github"
)

const (
	// security tokens
	ssmGitHubAppKey     = "github-sim-app-key"
	ssmCircleBuildToken = "circle_build_token_simulation"

	// Github app parameters
	startSimCmd      = "Start sim"
	ghCheckName      = "Long sim"
	ghConclusionFail = "failure" // The final conclusion of a failed github check

	// GitHub app installation details
	appIntegrationID  = 30867
	appInstallationID = 997580

	amiVersion = "master"

	// DynamoDB attribute and table names
	simStateTable = "SimulationState" // name of the dynamodb table where details from running simulations are stored
	primaryKey    = "SimLockID"       // primary partition key used by the sim state table
	attrStatus    = "Status"
	attrCheckID   = "CheckID"
	attrBuildURL  = "BuildURL"
	attrRepoOwner = "RepoOwner"
	attrRepoName = "RepoName"

	// simulation parameters
	blocks  = "100"
	genesis = "false"
)

type CircleApiPayload struct {
	Revision string          `json:"revision"`
	Params   BuildParameters `json:"build_parameters"`
}

type BuildParameters struct {
	CommitHash string `json:"GAIA_COMMIT_HASH"`
	Blocks     string `json:"BLOCKS"`
	Genesis    string `json:"GENESIS"`
	CheckID    string `json:"CHECK_ID"`
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

func handler(request events.APIGatewayProxyRequest) (proxyResp events.APIGatewayProxyResponse, err error) {
	var (
		buildURL, checkRunID string
		ghAppKey             *ssm.GetParameterOutput
		listCheckRunsResult  *gh.ListCheckRunsResults
		ghEvt                GithubEventPayload
		pr                   *gh.PullRequest
		checkRun             *gh.CheckRun
		transport            *ghapp.Transport
		sessionDDB           = ddb.New(session.Must(session.NewSession()))
		sessionSSM           = ssm.New(session.Must(session.NewSession()))
	)

	if err = json.Unmarshal([]byte(request.Body), &ghEvt); err != nil {
		log.Printf("INFO: github request: %+v", request.Body)
		return buildProxyResponse(500, fmt.Sprintf("ERROR: unmarshal github request: %s", err.Error())), err
	}
	if ghEvt.Issue.Pr.Url == "" {
		return buildProxyResponse(200, fmt.Sprint("INFO: not a PR comment")), err
	}
	if ghEvt.Comment.Body != startSimCmd {
		return buildProxyResponse(200, fmt.Sprintf("INFO: not a sim command")), err
	}

	if ghAppKey, err = getSsmParameter(sessionSSM, ssmGitHubAppKey); err != nil {
		return buildProxyResponse(500, fmt.Sprintf("ERROR: fetching github app key: %v", err)), err
	}
	if transport, err = ghapp.New(http.DefaultTransport, appIntegrationID, appInstallationID,
		[]byte(*ghAppKey.Parameter.Value)); err != nil {
		return buildProxyResponse(500, fmt.Sprintf("ERROR: authenticating github app: %v", err)), err
	}

	ghClient := gh.NewClient(&http.Client{Transport: transport})
	if pr, _, err = ghClient.PullRequests.Get(context.Background(), ghEvt.Repo.Owner.Login, ghEvt.Repo.Name,
		ghEvt.Issue.Number); err != nil {
		return buildProxyResponse(500, fmt.Sprintf("ERROR: PullRequests.Get: %v", err)), err
	}

	if listCheckRunsResult, _, err = ghClient.Checks.ListCheckRunsForRef(context.Background(), ghEvt.Repo.Owner.Login,
		ghEvt.Repo.Name, *pr.Head.Ref, &gh.ListCheckRunsOptions{
			CheckName: aws.String(ghCheckName),
			Filter:    aws.String("latest"),
		}); err != nil {
		return buildProxyResponse(500, fmt.Sprintf("ERROR: listCheckRunsForRef: %v", err)), err
	}

	// if we can't find a previous check, create a new one
	if len(listCheckRunsResult.CheckRuns) == 0 || listCheckRunsResult.CheckRuns[0].GetConclusion() != "" {
		if checkRun, err = createCheckRun(ghClient, ghEvt, pr); err != nil {
			return buildProxyResponse(500, fmt.Sprintf("ERROR: createCheckRun: %v", err)), err
		}
	} else {
		checkRun = listCheckRunsResult.CheckRuns[0]
	}

	simState, err := getSimState(sessionDDB)
	if err != nil {
		_ = completeCheckRun(ghClient, checkRun.GetID(), ghConclusionFail,
			"Failed to retrieve current sim status", ghEvt.Repo.Owner.Login, ghEvt.Repo.Name)
		return buildProxyResponse(500, fmt.Sprintf("ERROR: getSimState: %v", err)), err
	}

	// check state to see if there is another sim in progress
	checkRunID = strconv.FormatInt(checkRun.GetID(), 10)
	if valStatus, ok := simState.Item[attrStatus]; ok {
		if *valStatus.S != "finished" {
			if valCheckID, ok := simState.Item[attrCheckID]; ok {
				switch *valCheckID.N {
				case checkRunID:
					if err = updateCheckRun(ghClient, checkRun.GetID(), ghEvt.Repo.Owner.Login, ghEvt.Repo.Name,
						"This simulation is currently in progress"); err != nil {
						log.Printf("ERROR: updateCheckRun: %v", err)
					}
					return buildProxyResponse(200, fmt.Sprint("INFO: simulation already in progress")), err
				default:
					if err = completeCheckRun(ghClient, checkRun.GetID(), ghConclusionFail,
						"Another simulation is already in progress", ghEvt.Repo.Owner.Login, ghEvt.Repo.Name); err != nil {
						log.Printf("ERROR: completeCheckRun: %v", err)
					}
					return buildProxyResponse(200, fmt.Sprint("INFO: another simulation already in progress")), err
				}
			}
		}
	}

	if buildURL, err = triggerCircleciJob(sessionSSM, CircleApiPayload{
		Revision: amiVersion,
		Params: BuildParameters{
			CommitHash: pr.Head.GetRef(),
			CheckID:    strconv.FormatInt(checkRun.GetID(), 10),
			Blocks:     blocks,
			Genesis:    genesis,
		}}); err != nil {
		_ = completeCheckRun(ghClient, checkRun.GetID(), ghConclusionFail,
			"Failed to trigger CircleCI build job", ghEvt.Repo.Owner.Login, ghEvt.Repo.Name)
		return buildProxyResponse(500, fmt.Sprintf("ERROR: trigger circleci job: %s", err.Error())), err
	}

	if err = putSimState(sessionDDB, "queued", checkRunID,ghEvt.Repo.Name, ghEvt.Repo.Name, buildURL); err != nil {
		log.Printf("ERROR: putSimState: %v", err)
	}

	err = updateCheckRun(ghClient, checkRun.GetID(), ghEvt.Repo.Owner.Login, ghEvt.Repo.Name,
		fmt.Sprintf("Image build in progress: %s", buildURL))

	return buildProxyResponse(200, fmt.Sprint("INFO: Init attempt finished")), err
}

type CircleJobTriggerResp struct {
	Status   string `json:"status"`
	Body     string `json:"body"`
	BuildURL string `json:"build_url"`
}

func triggerCircleciJob(sessionSSM *ssm.SSM, payload CircleApiPayload) (buildUrl string, err error) {
	var (
		circleToken *ssm.GetParameterOutput
		jsonPayload []byte
		request     *http.Request
		response    *http.Response
		data        CircleJobTriggerResp
		httpClient  = &http.Client{Timeout: 2 * time.Second}
	)

	if jsonPayload, err = json.Marshal(payload); err != nil {
		return buildUrl, err
	}
	if circleToken, err = getSsmParameter(sessionSSM, ssmCircleBuildToken); err != nil {
		return buildUrl, err
	}

	circleJobUrl := fmt.Sprintf("https://circleci.com/api/v1.1/project/github/tendermint/images/tree/%s?circle-token=%s",
		payload.Revision, *circleToken.Parameter.Value)
	if request, err = http.NewRequest("POST", circleJobUrl, bytes.NewBuffer(jsonPayload)); err != nil {
		return buildUrl, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "*/*") // without this header, CircleCI doesn't return valid JSON...
	if response, err = httpClient.Do(request); err != nil {
		return buildUrl, err
	}
	defer func() {
		closeErr := response.Body.Close()
		if err == nil {
			err = closeErr
		}
	}()

	err = json.NewDecoder(response.Body).Decode(&data)
	return data.BuildURL, err
}

func completeCheckRun(client *gh.Client, checkID int64, conclusion, summary, repoOwner, repoName string) (err error) {
	_, _, err = client.Checks.UpdateCheckRun(context.Background(), repoOwner, repoName, checkID,
		gh.UpdateCheckRunOptions{
			Name:       ghCheckName,
			Conclusion: aws.String(conclusion),
			Output: &gh.CheckRunOutput{
				Title:   aws.String("Details"),
				Summary: aws.String(summary),
			},
			CompletedAt: &gh.Timestamp{
				Time: time.Now()},
		})
	return err
}

func updateCheckRun(client *gh.Client, checkID int64, repoOwner, repoName, summary string) (err error) {
	_, _, err = client.Checks.UpdateCheckRun(context.Background(), repoOwner, repoName, checkID,
		gh.UpdateCheckRunOptions{
			Name: ghCheckName,
			Output: &gh.CheckRunOutput{
				Title:   aws.String("Details"),
				Summary: aws.String(summary),
			},
		})
	return err
}

func createCheckRun(ghClient *gh.Client, ghEvt GithubEventPayload, pr *gh.PullRequest) (*gh.CheckRun, error) {
	checkRun, _, err := ghClient.Checks.CreateCheckRun(context.Background(), ghEvt.Repo.Owner.Login, ghEvt.Repo.Name,
		gh.CreateCheckRunOptions{
			Name:       ghCheckName,
			Status:     aws.String("queued"),
			HeadBranch: pr.Head.GetRef(),
			HeadSHA:    pr.Head.GetSHA(),
		})
	return checkRun, err
}

func getSimState(sessionDDB *ddb.DynamoDB) (*ddb.GetItemOutput, error) {
	output, err := sessionDDB.GetItem(&ddb.GetItemInput{
		Key:       map[string]*ddb.AttributeValue{primaryKey: {S: aws.String("V1")}},
		TableName: aws.String(simStateTable),
	})
	return output, err
}

func putSimState(sessionDDB *ddb.DynamoDB, status, checkID, repoName, repoOwner, buildURL string) (err error) {
	_, err = sessionDDB.PutItem(&ddb.PutItemInput{
		Item: map[string]*ddb.AttributeValue{
			primaryKey:   {S: aws.String("V1")},
			attrStatus:   {S: aws.String(status)},
			attrCheckID:  {N: aws.String(checkID)},
			attrBuildURL: {S: aws.String(buildURL)},
			attrRepoName: {S: aws.String(repoName)},
			attrRepoOwner: {S: aws.String(repoOwner)},
		},
		TableName: aws.String(simStateTable),
	})
	return err
}

// get the value for parameterName from the AWS secure parameters store
func getSsmParameter(sessionSSM *ssm.SSM, parameterName string) (*ssm.GetParameterOutput, error) {
	getParamOutput, err := sessionSSM.GetParameter(&ssm.GetParameterInput{
		Name:           &parameterName,
		WithDecryption: aws.Bool(true),
	})
	return getParamOutput, err
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

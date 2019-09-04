package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/cosmos/tools/lib/runsimgh"
	"github.com/cosmos/tools/lib/runsimslack"
)

const (
	// EC2 config values
	awsRegion          = "us-east-1"
	ec2InstanceType    = "c4.8xlarge"
	ec2KeyPair         = "wallet-nodes"
	ec2InstanceProfile = "gaia-simulation"
	gaiaAmiIdPrefix    = "gaia-sim"

	// simulation config values
	genesisFilePath = "/home/ec2-user/genesis.json"

	// security token ID
	ghAppTokenID    = "github-sim-app-key"
	slackAppTokenID = "slack-app-key"

	slackIntegrationType = "slack"
	ghIntegrationType    = "github"
)

var (
	notifyOnly bool

	// simulation parameters
	blocks, period, seeds, sdkGitRev string
	genesis                          bool

	// ec2 instance properties
	shutdownBehavior string

	// integration variables and structs
	integrationType string
	github          = new(runsimgh.Integration)
	slack           = new(runsimslack.Integration)

	// CircleCI environment variables
	buildUrl, buildNum string
)

func init() {
	flag.BoolVar(&genesis, "Genesis", false, "Use genesis file in simulation")
	flag.BoolVar(&notifyOnly, "Notify", false, "Send notification and exit")

	blocks = os.Getenv("BLOCKS")
	period = os.Getenv("PERIOD")
	seeds = os.Getenv("SEEDS")

	// Not using os.LookupEnv on purpose to reduce the number of variables.
	integrationType = os.Getenv("INTEGRATION")

	shutdownBehavior = os.Getenv("SHUTDOWN_BEHAVIOR")
	buildUrl = os.Getenv("CIRCLE_BUILD_URL")
	buildNum = os.Getenv("CIRCLE_BUILD_NUM")
	sdkGitRev = os.Getenv("GAIA_COMMIT_HASH")
}

func main() {
	flag.Parse()

	if integrationType == ghIntegrationType {
		if err := github.ConfigFromState(awsRegion, ghAppTokenID); err != nil {
			log.Fatalf("ERROR: github.ConfigFromState: %v", err)
		}
		if err := github.SetActiveCheckRun(); err != nil || github.ActiveCheckRun == nil {
			log.Fatalf("ERROR: github.SetActiveCheckRun: %v", err)
		}
	} else if integrationType == slackIntegrationType {
		err := slack.ConfigFromState(awsRegion, slackAppTokenID)
		if err != nil {
			log.Fatalf("ERROR: slack.ConfigFromState: %v", err)
		}
	} else if integrationType == "CI" {
		log.Println("Just CircleCI things!")
		os.Exit(0)
	} else {
		log.Fatalf("ERROR: missing integration type parameter")
	}

	// Update github check or send slack message to notify that the image build has started.
	if notifyOnly {
		if integrationType == slackIntegrationType {
			pushNotification(false, buildInitMessage())
			os.Exit(0)
		}
		pushNotification(false, buildInitMessage())
		os.Exit(0)
	}

	sessionEC2 := ec2.New(session.Must(session.NewSession(&aws.Config{Region: aws.String(awsRegion)})))
	amiId, err := getAmiId(sdkGitRev, sessionEC2)
	if err != nil {
		cleanup()
		log.Fatalf("ERROR: getAmiId: %v", err)
	}
	if amiId == "" {
		cleanup()
		log.Fatalf("ERROR: simulation AMI not found")
	}

	seedLists := makeSeedLists(seeds)
	instanceIds := make([]*string, len(seedLists))
	msgQueue := make([]int, len(seedLists))
	for index := range seedLists {
		// Here be bash dragons. Modify with extreme caution. Ensure you add adequate line endings.
		// User data is a shell script that will be run during EC2 instance startup
		var userData strings.Builder
		userData.WriteString("#!/bin/bash \n")
		userData.WriteString("cd /home/ec2-user/go/src/github.com/cosmos/cosmos-sdk || exit 1\n")

		// Setup environment variables for golang.
		// Script can be found here: https://github.com/tendermint/images/blob/master/ami-gaia-sim/set_env.sh
		userData.WriteString("source /etc/profile.d/set_env.sh\n")
		userData.WriteString(buildRunsimCommand(seedLists[index], strconv.Itoa(index), buildNum))
		userData.WriteString("shutdown -h now")

		// Separate variable to make this code actually readable
		input := &ec2.RunInstancesInput{
			InstanceInitiatedShutdownBehavior: aws.String(shutdownBehavior),
			IamInstanceProfile:                &ec2.IamInstanceProfileSpecification{Name: aws.String(ec2InstanceProfile)},
			TagSpecifications: []*ec2.TagSpecification{{
				ResourceType: aws.String("instance"),
				Tags: []*ec2.Tag{{
					Key:   aws.String("Name"),
					Value: aws.String("SimID-" + buildNum)}}},
			},

			InstanceType: aws.String(ec2InstanceType),
			ImageId:      aws.String(amiId),
			KeyName:      aws.String(ec2KeyPair),
			MaxCount:     aws.Int64(1),
			MinCount:     aws.Int64(1),
			UserData:     aws.String(base64.StdEncoding.EncodeToString([]byte(userData.String()))),
		}

		ec2Reservation, err := sessionEC2.RunInstances(input)
		if err != nil {
			summary := fmt.Sprintf("ERROR: RunInstances: %v", err)

			// Checking aws error code to see if we have reached the EC2 limit for this instance type
			// Crashing out of the program is not desirable. We can run simulation with a lower number of seeds
			// TODO: make this more robust. If we reach the instance limit, switch to a different instance type/aws region
			if awsErr, ok := err.(awserr.Error); ok {
				if awsErr.Code() == "InstanceLimitExceeded" {
					// If no instances have been started yet, mark the simulation as failed
					if index == 1 {
						pushNotification(true, summary)
						cleanup()
						os.Exit(1)
					}
					// Continue the simulation with the seeds that have already started
					break
				}
			}
			// If it's not a limit error, crash out.
			// Terminate any instances that are already running, otherwise the github integration will possibly report
			// incorrect results.
			if index > 1 {
				terminateInstances(sessionEC2, instanceIds)
			}
			pushNotification(true, summary)
			cleanup()
			os.Exit(1)
		}
		// Saving these just in case we need to terminate them prematurely
		instanceIds[index] = ec2Reservation.Instances[0].InstanceId
		msgQueue[index] = index
	}

	if len(msgQueue) > 1 {
		sendSqsMsg(msgQueue)
	}
}

func makeSeedLists(seeds string) map[int]string {
	var str strings.Builder
	lists := make(map[int]string)
	seedsInt, err := strconv.Atoi(seeds)
	if err != nil {
		cleanup()
		log.Fatalf("ERROR: seed list contains invalid values")
	}

	index := 0
	for i := 0; i <= seedsInt; i++ {
		// TODO: make this a configurable value. Requires some more logic around AWS instance startup
		// Each machine has 36 cores. Allocate one core per seed
		if i != 0 && math.Mod(float64(i), 35) == 0 {
			lists[index] = strings.TrimRight(str.String(), ",")
			str.Reset()
			index++
		}
		str.WriteString(strconv.Itoa(i) + ",")
	}
	if str.String() != "" {
		lists[index] = strings.TrimRight(str.String(), ",")
	}
	return lists
}

func getAmiId(gitRevision string, svc *ec2.EC2) (amiID string, err error) {
	var imageID *ec2.DescribeImagesOutput
	input := &ec2.DescribeImagesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("name"),
				Values: []*string{aws.String(fmt.Sprintf("%s-%s", gaiaAmiIdPrefix, gitRevision))},
			}},
	}
	if imageID, err = svc.DescribeImages(input); err != nil {
		return
	}
	if len(imageID.Images) > 0 {
		amiID = *imageID.Images[0].ImageId
	}
	return
}

func terminateInstances(svc *ec2.EC2, instanceIds []*string) {
	if _, err := svc.TerminateInstances(&ec2.TerminateInstancesInput{
		InstanceIds: instanceIds,
	}); err != nil {
		log.Printf("ERROR: TerminateInstances: %v", err)
	}
}

func pushNotification(failed bool, message string) {
	// If the simulation failed, and pushing the notification also fails after this point, record the failure message.
	if failed {
		log.Print(message)
	}
	if integrationType == slackIntegrationType {
		if err := slack.PostMessage(message); err != nil {
			cleanup()
			log.Fatalf("ERROR: slack.PostMessage: %v", err)
		}
	} else if failed {
		if err := github.ConcludeCheckRun(&message, aws.String("failure")); err != nil {
			cleanup()
			log.Fatalf("ERROR: github.ConcludeCheckRun: %v", err)
		}
	} else {
		if err := github.UpdateCheckRunStatus(aws.String("in_progress"), &message); err != nil {
			cleanup()
			log.Fatalf("ERROR: github.UpdateCheckRunStatus: %v", err)
		}
	}
}

func buildRunsimCommand(seeds, hostId, simId string) string {
	logObjKey := fmt.Sprintf("sim-id-%s", os.Getenv("CIRCLE_BUILD_NUM"))
	integration := "-Github"
	if integrationType == slackIntegrationType {
		integration = "-Slack"
	}
	if genesis {
		return fmt.Sprintf("runsim -SimId %s -HostId %s -LogObjPrefix %s -SimAppPkg ./simapp %s -Seeds \"%s\" -Genesis %s %s %s TestFullAppSimulation;",
			simId, hostId, logObjKey, integration, seeds, genesisFilePath, blocks, period)
	}
	log.Printf("runsim -SimId %s -HostId %s -LogObjPrefix %s -SimAppPkg ./simapp %s -Seeds \"%s\" %s %s TestFullAppSimulation;",
		simId, hostId, logObjKey, integration, seeds, blocks, period)

	return fmt.Sprintf("runsim -SimId %s -HostId %s -LogObjPrefix %s -SimAppPkg ./simapp %s -Seeds \"%s\" %s %s TestFullAppSimulation;",
		simId, hostId, logObjKey, integration, seeds, blocks, period)
}

func buildInitMessage() string {
	return fmt.Sprintf("*ID #%s.* SDK hash/tag/branch: `%s`. <%s|Build URL>\nblocks:\t`%s`\nperiod:\t`%s`\nseeds:\t`%s`",
		buildNum, sdkGitRev, buildUrl, blocks, period, seeds)
}

// Function used if the program crashes out. Attempts to remove the state information from dynamoDB
func cleanup() {
	if integrationType == slackIntegrationType {
		if err := slack.DeleteState(); err != nil {
			log.Println(err)
		}
	} else {
		if err := github.DeleteState(); err != nil {
			log.Println(err)
		}
	}
}

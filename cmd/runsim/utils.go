package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/cosmos/tools/lib/runsimgh"
	"github.com/cosmos/tools/lib/runsimslack"
)

const queueNamePrefix = "gaia-sim-"

var (
	// file paths for the compressed logs
	okZip, failedZip, exportsZip string

	// integration types and parameters
	github    = new(runsimgh.Integration)
	slack     = new(runsimslack.Integration)
	awsRegion string
)

func configIntegration() {
	var err error
	awsRegion = os.Getenv("AWS_REGION")
	if awsRegion == "" {
		awsRegion = "us-east-1"
	}

	if notifyGithub {
		if err = github.ConfigFromState(awsRegion, ghAppTokenID); err != nil {
			log.Printf("ERROR: github.ConfigFromState: %v", err)
			uploadLogAndExit()
		}

		if err = github.SetActiveCheckRun(); err != nil {
			// Either lambda function or execmgmt would have failed if there was no active check run
			log.Printf("ERROR: github.SetActiveCheckRun: %v", err)
			uploadLogAndExit()
		}
	}

	if notifySlack {
		if err = slack.ConfigFromState(awsRegion, slackAppTokenID); err != nil {
			log.Printf("ERROR: slack.ConfigFromState: %v", err)
			uploadLogAndExit()
		}
	}
}

func publishResults(okSeeds, failedSeeds, exports []string) {
	err := compressLogs(okSeeds, failedSeeds, exports)
	if err != nil {
		pushNotification(true, fmt.Sprintf("Host %s: ERROR: compressLogs: %v\n", hostId, err))
		os.Exit(1)
	}

	objUrls, err := syncS3(okZip, failedZip, exportsZip)
	if err != nil {
		pushNotification(true, fmt.Sprintf("Host %s: ERROR: syncS3: %v\n", hostId, err))
		os.Exit(1)
	}

	if notifyGithub {
		if err := github.UpdateActiveCheckRun(); err != nil {
			log.Printf("ERROR: github.UpdateActiveCheckRun: %v", err)
		} else {
			pushNotification(len(failedSeeds) > 0, github.ActiveCheckRun.Output.GetSummary()+buildMessage(objUrls))
		}
	} else {
		pushNotification(len(failedSeeds) > 0, buildMessage(objUrls))
	}
	uploadLogAndExit()
}

func syncS3(fileNames ...string) (objUrls map[string]string, err error) {
	objUrls = make(map[string]string, len(fileNames))
	sessionS3 := s3.New(session.Must(session.NewSession(&aws.Config{Region: aws.String(awsRegion)})))

	var logBucket string
	if logBucket, err = getLogBucket(sessionS3); err != nil {
		return
	}

	if logObjPrefix == "" {
		logObjPrefix = "debug"
	}

	var file *os.File
	for _, fileName := range fileNames {
		_, err = os.Stat(fileName)
		if err == nil {
			if file, err = os.Open(fileName); err != nil {
				return
			}

			objKey := filepath.Join(logObjPrefix, hostId, filepath.Base(file.Name()))
			if _, err = sessionS3.PutObject(&s3.PutObjectInput{
				Body:   aws.ReadSeekCloser(file),
				Bucket: aws.String(logBucket),
				Key:    aws.String(objKey),
			}); err != nil {
				return
			}
			objUrls[fileName] = fmt.Sprintf("https://%s.s3.amazonaws.com/%s", logBucket, objKey)
		}
	}
	return
}

func getLogBucket(sessionS3 *s3.S3) (logBucket string, err error) {
	outputListBuckets, err := sessionS3.ListBuckets(&s3.ListBucketsInput{})
	if err != nil {
		return
	}

	for _, bucket := range outputListBuckets.Buckets {
		if strings.Contains(*bucket.Name, logBucketPrefix) {
			logBucket = *bucket.Name
			return
		}
	}
	return logBucket, errors.New("LogBucketNotFound")
}

func compressLogs(okSeeds, failedSeeds, exportsPaths []string) (err error) {
	var simExports []string
	// Export files may not exist if the simulation failed before they are created
	for _, path := range exportsPaths {
		_, err := os.Stat(path)
		if err == nil {
			simExports = append(simExports, path)
		}
	}

	if len(simExports) > 0 {
		if err = zipFiles(exportsZip, simExports); err != nil {
			return
		}
	}

	if len(okSeeds) > 0 {
		if err = zipFiles(okZip, okSeeds); err != nil {
			return
		}
	}

	if len(failedSeeds) > 0 {
		if err = zipFiles(failedZip, failedSeeds); err != nil {
			return
		}
	}
	return
}

// compresses one or many files into a single zip archive.
func zipFiles(filename string, files []string) (err error) {
	log.Println(filename)
	zipFile, err := os.Create(filename)
	if err != nil {
		return
	}
	defer func() {
		cerr := zipFile.Close()
		if err == nil {
			err = cerr
		}
	}()

	zipWriter := zip.NewWriter(zipFile)
	defer func() {
		cerr := zipWriter.Close()
		if err == nil {
			err = cerr
		}
	}()

	for _, file := range files {
		if err = addFileToZip(zipWriter, file); err != nil {
			return
		}
	}
	return
}

func addFileToZip(zipWriter *zip.Writer, fileName string) (err error) {
	file, err := os.Open(fileName)
	if err != nil {
		return err
	}

	// Get the file information
	info, err := file.Stat()
	if err != nil {
		return
	}

	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return
	}

	header.Name = file.Name()
	// change to deflate to gain better compression
	// see http://golang.org/pkg/archive/zip/#pkg-constants
	header.Method = zip.Deflate
	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		return err
	}

	if _, err = io.Copy(writer, file); err != nil {
		return
	}

	err = file.Close()
	return
}

func pushNotification(failed bool, message string) {
	last, lastCheckErr := checkIfLast()
	if notifySlack {
		if err := slack.PostMessage(message); err != nil {
			log.Printf("ERROR: slack.PostMessage: %v", err)
		}
		if last {
			err := slack.PostMessage("Simulation is finished!")
			if err != nil {
				log.Printf("ERROR: slack.PostMessage: %v", err)
			}
			_ = slack.DeleteState()
		}
	} else if notifyGithub {
		conclusion := "success"
		if lastCheckErr != nil {
			log.Printf("ERROR: checkIfLast: %v", lastCheckErr)
			conclusion = "neutral"
		} else if failed {
			conclusion = "failure"
		}
		if !last && !failed && lastCheckErr == nil {
			if err := github.UpdateCheckRunStatus(github.ActiveCheckRun.Status, &message); err != nil {
				log.Printf("ERROR: github.UpdateCheckRunStatus: %v", err)
			}
		} else {
			err := github.ConcludeCheckRun(&message, &conclusion)
			if err != nil {
				log.Printf("ERROR: github.ConcludeCheckRun: %v", err)
			}
			if last {
				_ = github.DeleteState()
			}
		}
	}
}

func buildMessage(objUrls map[string]string) (msg string) {
	var message strings.Builder

	message.WriteString(fmt.Sprintf("Host %s finished simulation. Logs: ", hostId))

	for name, objUrl := range objUrls {
		switch name {
		case okZip:
			if notifySlack {
				message.WriteString(fmt.Sprintf("<%s|OK> ", objUrl))
			} else if notifyGithub {
				message.WriteString(fmt.Sprintf("[OK](%s) ", objUrl))
			}
		case failedZip:
			if notifySlack {
				message.WriteString(fmt.Sprintf("*<%s|FAILED>* ", objUrl))
			} else if notifyGithub {
				message.WriteString(fmt.Sprintf("[**FAILED**](%s) ", objUrl))
			}
		case exportsZip:
			if notifySlack {
				message.WriteString(fmt.Sprintf("<%s|Exports> ", objUrl))
			} else if notifyGithub {
				message.WriteString(fmt.Sprintf("[Exports](%s) ", objUrl))
			}
		}
	}
	// Make it look nice in the github summary
	message.WriteString("\n")
	return message.String()
}

// checkIfLast will check the SQS queue for any messages.
// If there are no messages left, that means this is the last instance running a simulation. Return true.
func checkIfLast() (bool, error) {
	svc := sqs.New(session.Must(session.NewSession(&aws.Config{Region: aws.String(awsRegion)})))

	queues, err := svc.ListQueues(&sqs.ListQueuesInput{QueueNamePrefix: aws.String(queueNamePrefix)})
	if err != nil {
		return false, err
	}

	// TODO: add attributes that identify which simulation they belong to. Enables multiple sims to run in parallel
	receiveMsgOutput, err := svc.ReceiveMessage(&sqs.ReceiveMessageInput{
		QueueUrl:            queues.QueueUrls[0],
		MaxNumberOfMessages: aws.Int64(1),
	})
	if err != nil {
		return false, err
	}

	if len(receiveMsgOutput.Messages) > 0 {
		// Delete one message to indicate this instance is finished
		_, _ = svc.DeleteMessage(&sqs.DeleteMessageInput{
			QueueUrl:      queues.QueueUrls[0],
			ReceiptHandle: receiveMsgOutput.Messages[0].ReceiptHandle,
		})
		return false, err
	}
	return true, nil
}

// Attempt to push the runsim log to S3 before exiting
func uploadLogAndExit() {
	_ = runsimLogFile.Close()
	_, _ = syncS3(runsimLogFile.Name())
	os.Exit(1)
}

package runsimslack

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/cosmos/tools/lib/runsimaws"
	"github.com/nlopes/slack"
	"net/http"
	"time"
)

const primaryKey = "IntegrationType"
const tableName = "SimulationState"

type Integration struct {
	Client          *slack.Client
	State           *runsimaws.DdbTable
	IntegrationType *string
	MessageTS       *string
	ChannelID       *string
}

func (Slack *Integration) ConfigFromState(awsRegion, slackAppTokenID string) (err error) {
	Slack.State = new(runsimaws.DdbTable)
	Slack.State.Config(awsRegion, primaryKey, tableName)
	ssm := new(runsimaws.Ssm)
	ssm.Config(awsRegion)

	if err = Slack.State.GetState("Slack", Slack); err != nil {
		return err
	}

	token, err := ssm.GetParameter(slackAppTokenID)
	if err != nil {
		return err
	}

	if *Slack.MessageTS == "" {
		return errors.New("ErrorMissingAttribute: SlackMsgTS")
	}
	if *Slack.ChannelID == "" {
		return errors.New("ErrorMissingAttribute: SlackChannel")
	}
	Slack.Client = slack.New(token)
	return nil
}

func (Slack *Integration) ConfigFromScratch(awsRegion, channelId, slackAppTokenID string) (err error) {
	Slack.IntegrationType = aws.String("Slack")
	Slack.MessageTS = aws.String("")
	Slack.ChannelID = &channelId

	ssm := new(runsimaws.Ssm)
	ssm.Config(awsRegion)

	token, err := ssm.GetParameter(slackAppTokenID)
	Slack.Client = slack.New(token)
	return
}

func (Slack *Integration) PushSlackCmdReply(message, responseUrl string) (err error) {
	payload, err := json.Marshal(struct{Text string `json:"text"`}{Text: message})
	if err != nil {
		return err
	}

	request, err := http.NewRequest("POST", responseUrl, bytes.NewBuffer(payload))
	if err != nil {
		return err
	}

	request.Header.Set("Content-Type", "application/json;charset=UTF-8")
	httpClient := &http.Client{Timeout: 10 * time.Second}
	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}

	err = response.Body.Close()
	return
}

func (Slack *Integration) PostMessage(message string) (err error) {
	_, messageTS, err := Slack.Client.PostMessage(*Slack.ChannelID, slack.MsgOptionTS(*Slack.MessageTS),
		slack.MsgOptionText(message, false))
	if err != nil {
		return err
	}

	if Slack.MessageTS == nil {
		Slack.MessageTS = aws.String(messageTS)
	}
	return
}

func (Slack *Integration) DeleteState() (err error) {
	return Slack.State.DeleteState("Slack")
}

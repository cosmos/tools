package runsimslack

import (
	"errors"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/cosmos/tools/lib/runsimaws"
	"github.com/nlopes/slack"
)

const primaryKey = "IntegrationType"
const tableName = "SimulationState"

type Integration struct {
	Client    *slack.Client
	State     *runsimaws.DdbTable
	MessageTS *string
	ChannelID *string
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

func (Slack *Integration) ConfigFromScratch(awsRegion, messageTS, channelID, slackAppTokenID string) (err error) {
	Slack.MessageTS = &messageTS
	Slack.ChannelID = &channelID

	Slack.State = new(runsimaws.DdbTable)
	Slack.State.Config(awsRegion, primaryKey, tableName)

	if err = Slack.State.PutState(Slack); err != nil {
		return
	}

	ssm := new(runsimaws.Ssm)
	ssm.Config(awsRegion)

	token, err := ssm.GetParameter(slackAppTokenID)
	Slack.Client = slack.New(token)
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

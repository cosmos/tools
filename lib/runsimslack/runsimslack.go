package runsimslack

import (
	"errors"

	"github.com/cosmos/tools/lib/runsimaws"
	"github.com/nlopes/slack"
)

type Integration struct {
	Client    *slack.Client
	MessageTS *string
	ChannelID *string
}

func (Slack *Integration) ConfigFromState(awsRegion, slackAppTokenID string) (err error) {
	ddbTable := new(runsimaws.DdbTable)
	ssm := new(runsimaws.Ssm)
	ddbTable.Config(awsRegion, "IntegrationType", "SimulationState")
	ssm.Config(awsRegion)

	err = ddbTable.GetState("Slack", Slack)
	if err != nil {
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

func (Slack *Integration) ConfigFromScratch(awsRegion, messageTS, channelID, slackAppTokenID string) (err error){
	ssm := new(runsimaws.Ssm)
	ssm.Config(awsRegion)

	token, err := ssm.GetParameter(slackAppTokenID)
	Slack.Client = slack.New(token)
	return
}

func (Slack *Integration) PostMessage(message string) (err error) {
	_, _, err = Slack.Client.PostMessage(*Slack.ChannelID, slack.MsgOptionTS(*Slack.MessageTS),
		slack.MsgOptionText(message, false))
	if err != nil {
		return
	}
	return
}

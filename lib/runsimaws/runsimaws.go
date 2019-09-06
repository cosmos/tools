package runsimaws

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	ddb "github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"
	"github.com/aws/aws-sdk-go/service/ssm"
)

// DynamoDB utilities used by the runsim application
//

type DdbTable struct {
	svc        *ddb.DynamoDB
	PrimaryKey *string
	Name       *string
}

func (table *DdbTable) Config(awsRegion, primaryKey, tableName string) {
	table.PrimaryKey = &primaryKey
	table.Name = &tableName
	table.svc = ddb.New(session.Must(session.NewSession(&aws.Config{Region: aws.String(awsRegion)})))
}

func (table *DdbTable) GetState(key string, data interface{}) (err error) {
	queryResult, err := table.svc.GetItem(&ddb.GetItemInput{
		Key:       map[string]*ddb.AttributeValue{*table.PrimaryKey: {S: aws.String(key)}},
		TableName: aws.String(*table.Name),
	})
	if err != nil {
		return
	}
	err = dynamodbattribute.UnmarshalMap(queryResult.Item, &data)
	return
}

func (table *DdbTable) PutState(data interface{}) (err error) {
	attributes, err := dynamodbattribute.MarshalMap(data)
	if err != nil {
		return
	}
	_, err = table.svc.PutItem(&ddb.PutItemInput{
		TableName: table.Name,
		Item:      attributes,
	})
	return
}

func (table *DdbTable) DeleteState(key string) (err error) {
	_, err = table.svc.DeleteItem(&ddb.DeleteItemInput{
		Key:       map[string]*ddb.AttributeValue{*table.PrimaryKey: {S: aws.String(key)}},
		TableName: table.Name,
	})
	return
}

// SSM functions used by the runsim application
//

type Ssm struct {
	svc *ssm.SSM
}

func (params *Ssm) Config(awsRegion string) {
	params.svc = ssm.New(session.Must(session.NewSession(&aws.Config{Region: aws.String(awsRegion)})))
}

func (params *Ssm) GetParameter(parameterName string) (paramValue string, err error) {
	getParamOutput, err := params.svc.GetParameter(&ssm.GetParameterInput{
		Name:           &parameterName,
		WithDecryption: aws.Bool(true),
	})
	if err != nil {
		return
	}
	paramValue = *getParamOutput.Parameter.Value
	return
}

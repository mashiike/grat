package grat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

type BatchItemFailureResponse struct {
	BatchItemFailures []BatchItemFailureItem `json:"batchItemFailures,omitempty"`
}

type BatchItemFailureItem struct {
	ItemIdentifier string `json:"itemIdentifier"`
}

var MaxDeleteRetry = 8

// Run runs lambda handler on AWS Lambda runtime or sqs polling server.
func Run(queue string, batchSize int, handler interface{}, options ...lambda.Option) error {
	return RunWithContext(context.Background(), queue, batchSize, handler, options...)
}

// RunWithContext runs lambda handler on AWS Lambda runtime or sqs polling server with context.
func RunWithContext(ctx context.Context, queue string, batchSize int, handler interface{}, options ...lambda.Option) error {
	options = append(options, lambda.WithContext(ctx))
	if strings.HasPrefix(os.Getenv("AWS_EXECUTION_ENV"), "AWS_Lambda") || os.Getenv("AWS_LAMBDA_RUNTIME_API") != "" {
		log.Println("[info] run on AWS Lambda runtime")
		lambda.StartWithOptions(handler, options...)
		return nil
	} else {
		log.Println("[info] run as sqs polling server")
		return runOnLocal(ctx, queue, batchSize, handler, options...)
	}
}

func runOnLocal(ctx context.Context, queue string, batchSize int, handler interface{}, options ...lambda.Option) error {
	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return err
	}
	client := sqs.NewFromConfig(awsCfg)
	queueURL, queueARN, err := detectRecource(ctx, client, queue)
	if err != nil {
		return err
	}
	if batchSize < 1 {
		batchSize = 1
	}
	h := lambda.NewHandlerWithOptions(handler, options...)
	log.Printf("[info] starting polling %s", queueURL)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		receiveOutput, err := client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:              aws.String(queueURL),
			MaxNumberOfMessages:   int32(batchSize),
			WaitTimeSeconds:       1,
			AttributeNames:        []types.QueueAttributeName{"All"},
			MessageAttributeNames: []string{"All"},
		})
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			return err
		}
		if len(receiveOutput.Messages) == 0 {
			continue
		}
		log.Printf("[info] received %d messages", len(receiveOutput.Messages))
		event := &events.SQSEvent{
			Records: make([]events.SQSMessage, 0, len(receiveOutput.Messages)),
		}
		for _, message := range receiveOutput.Messages {
			record := convertMessageToEventRecord(queueARN, &message)
			event.Records = append(event.Records, *record)
		}
		payload, err := json.MarshalIndent(event, "", "  ")
		if err != nil {
			return err
		}

		handlerOutput, err := func() (_payload []byte, _err error) {
			defer func() {
				if perr := recover(); perr != nil {
					_err = fmt.Errorf("handler panic:%v", perr)
				}
			}()
			_payload, _err = h.Invoke(ctx, payload)
			return
		}()

		if err != nil {
			log.Printf("[error] handler error: %v", err)
			continue
		}
		var batchItemFailureResp BatchItemFailureResponse
		var successMessages []types.Message
		if err := json.Unmarshal(handlerOutput, &batchItemFailureResp); err == nil {
			for _, message := range receiveOutput.Messages {
				successMessages = make([]types.Message, 0, len(receiveOutput.Messages))
				var isFailure bool
				for _, failureItem := range batchItemFailureResp.BatchItemFailures {
					if message.MessageId == &failureItem.ItemIdentifier {
						isFailure = true
						break
					}
				}
				if !isFailure {
					successMessages = append(successMessages, message)
				}
			}
			log.Printf("[info] %d messages are success, %d messages are failure", len(successMessages), len(receiveOutput.Messages)-len(successMessages))
		} else {
			successMessages = receiveOutput.Messages
		}
		for _, message := range successMessages {
			_, err := client.DeleteMessage(context.Background(), &sqs.DeleteMessageInput{
				ReceiptHandle: message.ReceiptHandle,
				QueueUrl:      aws.String(queueURL),
			})
			if err != nil {
				log.Printf("[warn][%s] can't delete message. %v", *message.MessageId, err)
				for i := 1; i <= MaxDeleteRetry; i++ {
					log.Printf("[info][%s] retry to delete after %d sec.", *message.MessageId, i*i)
					time.Sleep(time.Duration(i*i) * time.Second)
					_, err = client.DeleteMessage(context.Background(), &sqs.DeleteMessageInput{
						ReceiptHandle: message.ReceiptHandle,
						QueueUrl:      aws.String(queueURL),
					})
					if err == nil {
						log.Printf("[info][%s] message was deleted successfuly.", *message.MessageId)
						break
					}
					log.Printf("[warn][%s] can't delete message. %s", *message.MessageId, err)
					if i == MaxDeleteRetry {
						log.Printf("[error] [%s] max retry count reached.", *message.MessageId)
					}
				}
			}
		}
	}
}

func detectRecource(ctx context.Context, client *sqs.Client, queue string) (string, *arn.ARN, error) {
	if arnObj, err := arn.Parse(queue); err == nil {
		urlObj := &url.URL{
			Scheme: "https",
			Host:   fmt.Sprintf("sqs.%s.amazonaws.com", arnObj.Region),
			Path:   fmt.Sprintf("/%s/%s", arnObj.AccountID, arnObj.Resource),
		}
		return urlObj.String(), &arnObj, nil
	}
	if urlObj, err := url.Parse(queue); err == nil && urlObj.Scheme == "https" {
		arnObj, err := convertQueueURLToARN(urlObj)
		if err != nil {
			return "", nil, err
		}
		return queue, arnObj, err
	}
	output, err := client.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{
		QueueName: aws.String(queue),
	})
	if err != nil {
		return "", nil, err
	}
	urlObj, err := url.Parse(*output.QueueUrl)
	if err != nil {
		return "", nil, err
	}
	arnObj, err := convertQueueURLToARN(urlObj)
	if err != nil {
		return "", nil, err
	}
	return *output.QueueUrl, arnObj, err
}

func convertQueueURLToARN(urlObj *url.URL) (*arn.ARN, error) {
	if !strings.HasSuffix(strings.ToLower(urlObj.Host), ".amazonaws.com") || !strings.HasPrefix(strings.ToLower(urlObj.Host), "sqs.") {
		return nil, errors.New("invalid queue url")
	}
	part := strings.Split(strings.TrimLeft(urlObj.Path, "/"), "/")
	if len(part) != 2 {
		return nil, errors.New("invalid queue url")
	}
	awsRegion := strings.TrimSuffix(strings.TrimPrefix(strings.ToLower(urlObj.Host), "sqs."), ".amazonaws.com")
	arnObj := &arn.ARN{
		Partition: "aws",
		Service:   "sqs",
		Region:    awsRegion,
		AccountID: part[0],
		Resource:  part[1],
	}
	return arnObj, nil
}

func convertMessageToEventRecord(queueARN *arn.ARN, message *types.Message) *events.SQSMessage {
	mesageAttributes := make(map[string]events.SQSMessageAttribute, len(message.MessageAttributes))
	for key, value := range message.MessageAttributes {
		mesageAttributes[key] = events.SQSMessageAttribute{
			StringValue:      value.StringValue,
			BinaryValue:      value.BinaryValue,
			StringListValues: value.StringListValues,
			BinaryListValues: value.BinaryListValues,
			DataType:         *value.DataType,
		}
	}
	return &events.SQSMessage{
		MessageId:              coalesce(message.MessageId),
		ReceiptHandle:          coalesce(message.ReceiptHandle),
		Body:                   coalesce(message.Body),
		Md5OfBody:              coalesce(message.MD5OfBody),
		Md5OfMessageAttributes: coalesce(message.MD5OfMessageAttributes),
		Attributes:             message.Attributes,
		MessageAttributes:      mesageAttributes,
		EventSourceARN:         queueARN.String(),
		EventSource:            "aws:sqs",
		AWSRegion:              queueARN.Region,
	}
}

func coalesce(strs ...*string) string {
	for _, str := range strs {
		if str != nil {
			return *str
		}
	}
	return ""
}

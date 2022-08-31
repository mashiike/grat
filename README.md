# grat

Integration of AWS Lambda and SQS Polling Server


## Example

grat provides transparent development with local and AWS Lambda runtime

```go
package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/aws/aws-lambda-go/events"
	"github.com/mashiike/grat"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)
	defer cancel()

	if err := grat.RunWithContext(ctx, "grat-hello", 10, handler); err != nil {
		log.Fatalln("[error]", err)
	}
	log.Println("[info] shutdown complate")
}

func handler(ctx context.Context, event *events.SQSEvent) error {
	log.Printf("[info] %d messages received", len(event.Records))
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(event); err != nil {
		return err
	}
	return nil 
}
```

1. Create IAM role "grat" for Lambda which have attached policy AWSLambdaSQSQueueExecutionRole.
1. Install [lambroll](https://github.com/fujiwara/lambroll).
1. Place main.go to example/.
1. Run `make deploy` to deploy a lambda function.
1. Create SQS queue `grat-hello` and set event source mapping.
  - https://docs.aws.amazon.com/lambda/latest/dg/with-sqs.html


### grat.RunWithContext(ctx, queue, batchSize, handler)

`grat.RunWithContext(ctx, queue, batchSize, handler)` works as below.

- If a process is running on Lambda (`AWS_EXECUTION_ENV` or `AWS_LAMBDA_RUNTIME_API` environment variable defined),
  - Call lambda.StartWithOptions()
- Otherwise start a sqs polling server using queue and batchSize.

## LICENSE

The MIT License 

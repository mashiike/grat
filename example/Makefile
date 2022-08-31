export AWS_ACCOUNT_ID := $(shell aws sts get-caller-identity --query Account --output text)
export AWS_DEFAULT_REGION := ap-northeast-1
bootstrap: *.go
	GOOS=linux GOARCH=amd64 go build -o bootstrap main.go

deploy: bootstrap function.json
	lambroll deploy

clean:
	rm -f bootstrap

logs:
	lambroll logs --follow

create-event-source-mapping:
	aws lambda create-event-source-mapping \
		--function-name grat-hello --batch-size 5 \
		--maximum-batching-window-in-seconds 60 \
		--event-source-arn arn:aws:sqs:$(AWS_DEFAULT_REGION):$(AWS_ACCOUNT_ID):grat-hello

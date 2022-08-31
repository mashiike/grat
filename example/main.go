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

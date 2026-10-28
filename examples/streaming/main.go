package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/obediencecorp/obey-go-sdk/pkg/obey"
)

func main() {
	provider := flag.String("provider", "", "operator-selected provider")
	model := flag.String("model", "", "operator-selected model")
	flag.Parse()
	if *provider == "" || *model == "" {
		log.Fatal("--provider and --model are required")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	client, err := obey.NewClientFromPath()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()
	stream, err := client.StreamPrompt(ctx, "Explain the current project", &obey.RunOptions{SessionOptions: obey.SessionOptions{Provider: *provider, Model: *model}})
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()
	log.Printf("fidelity=%s", stream.Fidelity())
	for stream.Next() {
		event := stream.Event()
		if event.Completion != nil {
			fmt.Print(event.Completion.Text)
		}
	}
	// Result also drains bounded CLI cleanup after cancellation and returns any
	// partial identity; Err alone can be inspected by event-only consumers.
	result, err := stream.Result()
	if err != nil {
		if result != nil {
			log.Printf("session=%s cleanup=%s", result.Session.SessionID, result.Cleanup)
		}
		log.Fatal(err)
	}
}

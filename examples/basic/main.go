package main

import (
	"context"
	"errors"
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
	camp := flag.String("camp", "", "explicit camp or CLI cwd inference")
	flag.Parse()
	if *provider == "" || *model == "" {
		log.Fatal("--provider and --model are required")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	client, err := obey.NewClientFromPath(obey.WithCamp(*camp))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()
	result, err := client.RunPrompt(ctx, "Explain the current project", &obey.RunOptions{SessionOptions: obey.SessionOptions{Provider: *provider, Model: *model}})
	if err != nil {
		var failure *obey.Error
		if errors.As(err, &failure) {
			log.Printf("code=%s session=%s", failure.Code, failure.Session.SessionID)
		}
		log.Fatal(err)
	}
	fmt.Println(result.Text)
}

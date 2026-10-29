package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log"
	"net/http"

	"github.com/Obedience-Corp/obey-go-sdk/pkg/obey"
)

func handler(client *obey.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		prompt, err := io.ReadAll(io.LimitReader(r.Body, (10<<20)+1))
		if err != nil || len(prompt) > 10<<20 {
			http.Error(w, "invalid prompt", http.StatusBadRequest)
			return
		}
		result, err := client.RunPrompt(r.Context(), string(prompt), nil)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			code := "command_error"
			var failure *obey.Error
			if errors.As(err, &failure) {
				code = failure.Code
			}
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"text": result.Text, "session_id": result.Session.SessionID})
	}
}
func main() {
	provider := flag.String("provider", "", "operator-selected provider")
	model := flag.String("model", "", "operator-selected model")
	camp := flag.String("camp", "", "explicit camp")
	flag.Parse()
	if *provider == "" || *model == "" {
		log.Fatal("--provider and --model are required")
	}
	client, err := obey.NewClientFromPath(obey.WithCamp(*camp), obey.WithSessionDefaults(obey.SessionOptions{Provider: *provider, Model: *model}))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()
	http.HandleFunc("/run", handler(client))
	log.Fatal(http.ListenAndServe("127.0.0.1:8080", nil))
}

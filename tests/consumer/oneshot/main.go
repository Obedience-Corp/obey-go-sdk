package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	"github.com/Obedience-Corp/obey-go-sdk/pkg/obey"
)

type response struct {
	Result   *obey.Result `json:"result"`
	Code     string       `json:"error_code,omitempty"`
	Deadline bool         `json:"deadline_exceeded,omitempty"`
	Canceled bool         `json:"canceled,omitempty"`
	ExitCode *int         `json:"exit_code,omitempty"`
}

// execute is injected into a job or request handler; its caller owns context.
func execute(ctx context.Context, client *obey.Client, prompt string, service bool) response {
	var result *obey.Result
	var err error
	if service {
		stream, e := client.StreamPrompt(ctx, prompt, nil)
		if e != nil {
			err = e
		} else {
			result, err = stream.Result()
		}
	} else {
		result, err = client.RunPrompt(ctx, prompt, nil)
	}
	out := response{Result: result, Deadline: errors.Is(err, context.DeadlineExceeded), Canceled: errors.Is(err, context.Canceled)}
	if err != nil {
		var failure *obey.Error
		if errors.As(err, &failure) {
			out.Code = failure.Code
			out.ExitCode = failure.ExitCode
		} else {
			out.Code = "untyped_error"
		}
	}
	return out
}
func main() {
	binary := flag.String("binary", "/usr/local/bin/obey", "CLI artifact path")
	socket := flag.String("socket", "/tmp/daemon.sock", "isolated daemon socket")
	camp := flag.String("camp", "", "explicit synthetic camp")
	provider := flag.String("provider", "crash-provider", "deterministic fixture provider")
	model := flag.String("model", "crash-model", "deterministic fixture model")
	mode := flag.String("mode", "basic", "basic or service")
	deadline := flag.Duration("deadline", 30*time.Second, "request deadline")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), *deadline)
	defer cancel()
	client, err := obey.NewClient(*binary, obey.WithSocket(*socket), obey.WithCamp(*camp), obey.WithSessionDefaults(obey.SessionOptions{Provider: *provider, Model: *model, WorkingDirectory: "/data/work/" + *camp}))
	if err != nil {
		_ = json.NewEncoder(os.Stdout).Encode(response{Code: "construction_error"})
		os.Exit(1)
	}
	defer client.Close()
	var outcome response
	if *mode == "service" {
		request := httptest.NewRequest(http.MethodPost, "http://localhost/run", strings.NewReader("synthetic consumer request")).WithContext(ctx)
		recorder := httptest.NewRecorder()
		handler := func(w http.ResponseWriter, r *http.Request) {
			prompt, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "read request", 400)
				return
			}
			_ = json.NewEncoder(w).Encode(execute(r.Context(), client, string(prompt), false))
		}
		handler(recorder, request)
		if err := json.Unmarshal(recorder.Body.Bytes(), &outcome); err != nil {
			panic(err)
		}
	} else {
		outcome = execute(ctx, client, "synthetic consumer request", false)
	}
	_ = json.NewEncoder(os.Stdout).Encode(outcome)
}

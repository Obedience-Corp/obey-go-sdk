package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"time"

	"github.com/obediencecorp/obey-go-sdk/pkg/obey"
)

type response struct {
	Result                  *obey.Result         `json:"result,omitempty"`
	Transcript              *obey.TranscriptPage `json:"transcript,omitempty"`
	Receipt                 *obey.Acceptance     `json:"receipt,omitempty"`
	ErrorKey                string               `json:"error_key,omitempty"`
	ErrorClientRef          string               `json:"error_client_ref,omitempty"`
	AcceptanceIndeterminate bool                 `json:"acceptance_indeterminate,omitempty"`
	Deadline                bool                 `json:"deadline_exceeded,omitempty"`
	Canceled                bool                 `json:"canceled,omitempty"`
	Diagnostics             []obey.Diagnostic    `json:"diagnostics,omitempty"`
	Status                  *obey.DaemonStatus   `json:"status,omitempty"`
	Session                 *obey.SessionRef     `json:"session,omitempty"`
	Ref                     *obey.TurnRef        `json:"ref,omitempty"`
	State                   *obey.TurnState      `json:"state,omitempty"`
	AcceptanceNil           bool                 `json:"acceptance_nil"`
	Code                    string               `json:"error_code,omitempty"`
	DaemonCode              *int32               `json:"daemon_code,omitempty"`
	GRPCCode                string               `json:"grpc_code,omitempty"`
	ExitCode                *int                 `json:"exit_code,omitempty"`
	Raw                     json.RawMessage      `json:"raw,omitempty"`
}

func main() {
	if os.Getenv("OBEY_SDK_TEST_CONTAINER") != "1" {
		panic("persistent acceptance consumer requires isolated fixture")
	}
	binary := flag.String("binary", "/usr/local/bin/obey", "installed CLI")
	socket := flag.String("socket", "/tmp/daemon.sock", "isolated socket")
	mode := flag.String("mode", "open-turn", "create/open-session/open-turn")
	camp := flag.String("camp", "", "camp name for create, exact camp ID for handles")
	sessionID := flag.String("session", "", "exact session ID")
	turnID := flag.String("turn", "", "exact turn ID")
	effort := flag.String("reasoning-effort", "", "explicit session effort")
	workspace := flag.String("workspace", "", "agent workspace, independent from child cwd")
	refFile := flag.String("ref-file", "", "reopen serialized TurnRef from prior application process")
	refOutput := flag.String("ref-output", "", "serialize opened TurnRef before application exit")
	sessionMode := flag.String("session-mode", "", "initial session mode")
	sendMode := flag.String("send-mode", "", "persistent send mode")
	sendModel := flag.String("send-model", "", "per-turn model")
	key := flag.String("key", "", "caller-persisted idempotency key")
	clientRef := flag.String("client-ref", "", "caller correlation reference")
	promptFile := flag.String("prompt-file", "", "private prompt file")
	deadline := flag.Duration("deadline", 30*time.Second, "request deadline")
	pageSize := flag.Uint("page-size", 2, "explicit transcript page size")
	cursor := flag.String("cursor", "", "opaque transcript cursor")
	cancelAfter := flag.Bool("cancel-after-send", false, "cancel caller after receipt return")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), *deadline)
	defer cancel()
	client, err := obey.NewClient(*binary, obey.WithSocket(*socket), obey.WithCamp(*camp), obey.WithSessionDefaults(obey.SessionOptions{Provider: "crash-provider", Model: "crash-model", WorkingDirectory: *workspace, ReasoningEffort: *effort, Mode: *sessionMode}))
	if err != nil {
		panic(err)
	}
	defer client.Close()
	out := response{}
	switch *mode {
	case "status":
		out.Status, err = client.Status(ctx)
	case "create":
		var session *obey.Session
		session, err = client.CreateSession(ctx, nil)
		if session != nil {
			ref := session.Ref()
			out.Session = &ref
		}
	case "send":
		var session *obey.Session
		session, err = client.OpenSession(ctx, obey.SessionRef{CampID: *camp, SessionID: *sessionID})
		if err == nil {
			prompt := "SDK durable fixture prompt"
			if *promptFile != "" {
				data, readErr := os.ReadFile(*promptFile)
				if readErr != nil {
					panic(readErr)
				}
				prompt = string(data)
			}
			var options *obey.SendOptions
			if *key != "" || *clientRef != "" || *sendMode != "" || *sendModel != "" {
				options = &obey.SendOptions{IdempotencyKey: *key, ClientRef: *clientRef, Mode: *sendMode, Model: *sendModel}
			}
			var turn *obey.Turn
			turn, err = session.Send(ctx, prompt, options)
			if turn != nil {
				ref := turn.Ref()
				out.Ref = &ref
				out.Receipt = turn.Acceptance()
			}
			if *cancelAfter {
				cancel()
			}
		}
	case "transcript":
		var session *obey.Session
		session, err = client.OpenSession(ctx, obey.SessionRef{CampID: *camp, SessionID: *sessionID})
		if err == nil {
			out.Transcript, err = session.Transcript(ctx, obey.TranscriptOptions{PageSize: uint32(*pageSize), Cursor: *cursor})
		}
	case "open-session":
		var session *obey.Session
		session, err = client.OpenSession(ctx, obey.SessionRef{CampID: *camp, SessionID: *sessionID})
		if session != nil {
			ref := session.Ref()
			out.Session = &ref
		}
	default:
		ref := obey.TurnRef{SessionRef: obey.SessionRef{CampID: *camp, SessionID: *sessionID}, TurnID: *turnID}
		if *refFile != "" {
			data, readErr := os.ReadFile(*refFile)
			if readErr != nil {
				panic(readErr)
			}
			if json.Unmarshal(data, &ref) != nil {
				panic("invalid persisted ref")
			}
		}
		var turn *obey.Turn
		turn, err = client.OpenTurn(ctx, ref)
		if turn != nil {
			copy := turn.Ref()
			out.Ref = &copy
			out.AcceptanceNil = turn.Acceptance() == nil
			if *mode == "wait" {
				out.Result, err = turn.Wait(ctx)
			} else {
				out.State, err = turn.State(ctx)
			}

		}
	}
	out.Deadline = errors.Is(err, context.DeadlineExceeded)
	out.Canceled = errors.Is(err, context.Canceled)
	if *refOutput != "" && out.Ref != nil {
		data, encodeErr := json.Marshal(out.Ref)
		if encodeErr != nil {
			panic(encodeErr)
		}
		if writeErr := os.WriteFile(*refOutput, data, 0600); writeErr != nil {
			panic(writeErr)
		}
	}
	if err != nil {
		var typed *obey.Error
		if errors.As(err, &typed) {
			out.Code = typed.Code
			out.DaemonCode = typed.DaemonCode
			out.GRPCCode = typed.GRPCCode
			out.ExitCode = typed.ExitCode
			out.Raw = typed.RawRecord
			out.ErrorKey = typed.IdempotencyKey
			out.ErrorClientRef = typed.ClientRef
			out.AcceptanceIndeterminate = typed.AcceptanceIndeterminate
			out.Diagnostics = typed.Diagnostics
		} else {
			out.Code = "untyped_error"
		}
	}
	if err := client.Close(); err != nil {
		panic(err)
	} // Prove process-separated reopen, with no lingering CLI children.
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		panic(err)
	}
}

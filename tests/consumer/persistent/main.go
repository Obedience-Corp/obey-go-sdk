package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/obediencecorp/obey-go-sdk/pkg/obey"
)

type response struct {
	Approvals                         []obey.Approval    `json:"approvals,omitempty"`
	Cancel                            *obey.CancelResult `json:"cancel,omitempty"`
	DecisionIndeterminate             bool               `json:"decision_indeterminate,omitempty"`
	CallID                            string             `json:"call_id,omitempty"`
	CursorErrorCode, CursorObserved   string
	CursorRaw                         json.RawMessage
	Events                            []obey.Event        `json:"events,omitempty"`
	Fidelity                          obey.StreamFidelity `json:"fidelity,omitempty"`
	Stage                             string
	PipeWriteBlocked, ProbeFDClosed   bool
	PipeBytes, PipeCapacity, ChildPID int
	CloseMilliseconds                 int64
	Result                            *obey.Result         `json:"result,omitempty"`
	Transcript                        *obey.TranscriptPage `json:"transcript,omitempty"`
	Receipt                           *obey.Acceptance     `json:"receipt,omitempty"`
	ErrorKey                          string               `json:"error_key,omitempty"`
	ErrorClientRef                    string               `json:"error_client_ref,omitempty"`
	AcceptanceIndeterminate           bool                 `json:"acceptance_indeterminate,omitempty"`
	Deadline                          bool                 `json:"deadline_exceeded,omitempty"`
	Canceled                          bool                 `json:"canceled,omitempty"`
	Diagnostics                       []obey.Diagnostic    `json:"diagnostics,omitempty"`
	Status                            *obey.DaemonStatus   `json:"status,omitempty"`
	Session                           *obey.SessionRef     `json:"session,omitempty"`
	Ref                               *obey.TurnRef        `json:"ref,omitempty"`
	State                             *obey.TurnState      `json:"state,omitempty"`
	AcceptanceNil                     bool                 `json:"acceptance_nil"`
	Code                              string               `json:"error_code,omitempty"`
	DaemonCode                        *int32               `json:"daemon_code,omitempty"`
	GRPCCode                          string               `json:"grpc_code,omitempty"`
	ExitCode                          *int                 `json:"exit_code,omitempty"`
	Raw                               json.RawMessage      `json:"raw,omitempty"`
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
	backfill := flag.Uint("backfill-limit", 0, "requested attach backfill cap")
	noBackfill := flag.Bool("no-backfill", false, "skip stored attach rows")
	fromSequence := flag.Uint64("from-sequence", 0, "explicit durable cursor in this session scope")
	readyFile := flag.String("ready-file", "/data/sdk-attach-ready", "retained-handle fixture readiness")
	releaseFile := flag.String("release-file", "/data/sdk-attach-release", "retained-handle fixture release")
	follow := flag.Bool("follow", false, "observe live, detach after hello+join")
	pageSize := flag.Uint("page-size", 2, "explicit transcript page size")
	cursor := flag.String("cursor", "", "opaque transcript cursor")
	cancelAfter := flag.Bool("cancel-after-send", false, "cancel caller after receipt return")
	callID := flag.String("call", "", "exact approval call ID")
	deny := flag.Bool("deny", false, "explicit denial")
	reason := flag.String("reason", "consumer explicit decision", "decision reason")
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
	case "pending", "approve", "stop":
		var session *obey.Session
		session, err = client.OpenSession(ctx, obey.SessionRef{CampID: *camp, SessionID: *sessionID})
		if err == nil {
			switch *mode {
			case "pending":
				out.Approvals, err = session.PendingApprovals(ctx)
			case "approve":
				err = session.RespondApproval(ctx, obey.ApprovalDecision{CallID: *callID, Approve: !*deny, Reason: *reason})
			case "stop":
				err = session.Stop(ctx)
			}
		}
	case "cancel":
		var turn *obey.Turn
		turn, err = client.OpenTurn(ctx, obey.TurnRef{SessionRef: obey.SessionRef{CampID: *camp, SessionID: *sessionID}, TurnID: *turnID})
		if err == nil {
			out.Cancel, err = turn.Cancel(ctx)
		}
	case "rich-run":
		var stream *obey.RunStream
		stream, err = client.StreamPrompt(ctx, "machine-sdk-rich: deterministic typed/raw fixture", &obey.RunOptions{RequireLiveEvents: true})
		if err == nil {
			out.Fidelity = stream.Fidelity()
			for stream.Next() {
				event := stream.Event()
				if len(event.RawPayload) > 0 {
					event.RawPayload[0] = ' '
					if stream.Event().RawPayload[0] != '{' {
						panic("raw Event aliases")
					}
				}
				out.Events = append(out.Events, stream.Event())
			}
			out.Result, err = stream.Result()
		}
	case "attach", "attach-held", "observe":
		var stream *obey.EventStream
		var session *obey.Session
		if *mode == "attach" || *mode == "attach-held" {
			session, err = client.OpenSession(ctx, obey.SessionRef{CampID: *camp, SessionID: *sessionID})
			if err == nil && *mode == "attach-held" {
				data, _ := json.Marshal(session.Ref())
				if writeErr := os.WriteFile(*readyFile+".tmp", data, 0600); writeErr != nil {
					panic(writeErr)
				}
				if renameErr := os.Rename(*readyFile+".tmp", *readyFile); renameErr != nil {
					panic(renameErr)
				}
				for ctx.Err() == nil {
					if _, releaseErr := os.Stat(*releaseFile); releaseErr == nil {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				if ctx.Err() != nil {
					err = ctx.Err()
				}
			}
			if err == nil {
				stream, err = session.Attach(ctx, obey.AttachOptions{FromSequence: *fromSequence, BackfillLimit: uint32(*backfill), NoBackfill: *noBackfill})
			}
		} else {
			options := obey.ObserveOptions{Camp: *camp, Tail: uint32(*pageSize), Cursor: *cursor, Follow: *follow}
			if *sessionID != "" {
				options.SessionIDs = []string{*sessionID}
			}
			if *follow {
				options.Tail = 0
			}
			stream, err = client.Observe(ctx, options)
		}
		if err == nil {
			closed := false
			for stream.Next() {
				event := stream.Event()
				out.Events = append(out.Events, event)
				if !closed && (((*mode == "attach" || *mode == "attach-held") && event.Marker != nil) || (*mode == "observe" && *follow && len(out.Events) >= 2)) {
					_ = stream.Close()
					closed = true
				}
			}
			err = stream.Err()
			_ = stream.Close()
		}
		if *mode == "attach-held" && session != nil && stream != nil {
			cursorStream, cursorErr := session.Attach(ctx, obey.AttachOptions{FromSequence: 1})
			if cursorStream != nil {
				_ = cursorStream.Close()
				out.CursorErrorCode = "unexpected_success"
			}
			var typed *obey.Error
			if errors.As(cursorErr, &typed) {
				out.CursorErrorCode = typed.Code
				out.CursorObserved = typed.ObservedSessionID
				out.CursorRaw = typed.RawRecord
			}
		}

	case "unread-stream-close", "unread-client-close":
		var stream *obey.RunStream
		stream, err = client.StreamPrompt(ctx, "machine-run-unread: bounded pipe fixture", &obey.RunOptions{RequireLiveEvents: true})
		if err == nil {
			var metricErr error
			stage := func(name string) {
				out.Stage = name
				data, _ := json.Marshal(out)
				if writeErr := os.WriteFile("/data/sdk-unread-stage.json", data, 0600); writeErr != nil {
					panic(writeErr)
				}
			}
			stage("probing")
			out.ChildPID, out.PipeBytes, out.PipeCapacity, metricErr = waitForFullRunPipe(ctx)
			if metricErr != nil {
				panic(metricErr)
			}
			out.PipeWriteBlocked = true
			out.ProbeFDClosed = true
			stage("closing")
			start := time.Now()
			if *mode == "unread-client-close" {
				err = client.Close()
			} else {
				err = stream.Close()
			}
			out.CloseMilliseconds = time.Since(start).Milliseconds()
			stage("closed")
			if err == nil {
				out.Result, err = stream.Result()
			}
			stage("result")
		}

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
			out.DecisionIndeterminate = typed.DecisionIndeterminate
			out.CallID = typed.CallID
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

// Linux-only fixture instrumentation: query the SDK-owned child's stdout pipe
// without reading a byte. This proves unread backpressure before Close signals it.
func waitForFullRunPipe(ctx context.Context) (int, int, int, error) {
	for ctx.Err() == nil {
		tasks, _ := filepath.Glob("/proc/self/task/*/children")
		for _, task := range tasks {
			data, _ := os.ReadFile(task)
			for _, pidText := range strings.Fields(string(data)) {
				cmdline, _ := os.ReadFile("/proc/" + pidText + "/cmdline")
				if !strings.Contains(string(cmdline), "run\x00") || !strings.Contains(string(cmdline), "--protocol-version\x002") {
					continue
				}
				pipe, err := os.OpenFile("/proc/"+pidText+"/fd/1", os.O_RDONLY|syscall.O_NONBLOCK, 0)
				if err != nil {
					continue
				}
				capacity, _, capacityErr := syscall.Syscall(syscall.SYS_FCNTL, pipe.Fd(), syscall.F_GETPIPE_SZ, 0)
				var pending int32
				_, _, queryErr := syscall.Syscall(syscall.SYS_IOCTL, pipe.Fd(), syscall.TIOCINQ, uintptr(unsafe.Pointer(&pending)))
				_ = pipe.Close()
				blocked := false
				threads, _ := filepath.Glob("/proc/" + pidText + "/task/*/wchan")
				for _, thread := range threads {
					where, _ := os.ReadFile(thread)
					if strings.Contains(string(where), "pipe_write") {
						blocked = true
						break
					}
				}
				if capacityErr == 0 && queryErr == 0 && capacity > 0 && pending > 0 && int(pending) <= int(capacity) && blocked {
					pid, _ := strconv.Atoi(pidText)
					return pid, int(pending), int(capacity), nil
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return 0, 0, 0, fmt.Errorf("SDK-owned run stdout never observed blocked with occupied pipe: %w", ctx.Err())
}

//go:build process_tests

package obey

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/obediencecorp/obey-go-sdk/internal/execclient"
)

func processSendSession(t *testing.T, mode string, options ...ClientOption) (*Session, string) {
	t.Helper()
	client, ledger := handlesClient(t, mode, options...)
	return &Session{client: client, ref: SessionRef{CampID: handleCamp, SessionID: "session"}}, ledger
}
func TestProcessSendReceiptSnapshotsAndPrivateInput(t *testing.T) {
	publicGuard(t)
	for _, mode := range []string{"send-normal", "send-duplicate"} {
		t.Run(mode, func(t *testing.T) {
			session, ledger := processSendSession(t, mode)
			options := &SendOptions{IdempotencyKey: "persisted-key", ClientRef: "caller-ref", Mode: "plan", Model: "per-turn-model"}
			prompt := "private prompt\nexact UTF8: λ"
			turn, err := session.Send(t.Context(), prompt, options)
			if err != nil || turn == nil {
				t.Fatalf("turn=%v err=%v", turn, err)
			}
			receipt := turn.Acceptance()
			if receipt == nil || receipt.Ref != turn.Ref() || turn.Ref().IdempotencyKey != options.IdempotencyKey || turn.Ref().ClientRef != options.ClientRef || turn.Ref().TurnID != "turn" || !strings.Contains(string(receipt.Raw), "future") {
				t.Fatalf("receipt=%+v ref=%+v", receipt, turn.Ref())
			}
			if mode == "send-normal" && (receipt.Status != TurnQueued || receipt.QueuePosition == nil || *receipt.QueuePosition != 0) {
				t.Fatalf("queued zero lost: %+v", receipt)
			}
			if mode == "send-duplicate" && (receipt.Status != TurnCompleted || !receipt.Deduplicated || receipt.QueuePosition != nil) {
				t.Fatalf("duplicate snapshot=%+v", receipt)
			}
			receipt.Raw[0] = 'x'
			if receipt.QueuePosition != nil {
				*receipt.QueuePosition = 9
			}
			if turn.Acceptance().Raw[0] != '{' || (mode == "send-normal" && *turn.Acceptance().QueuePosition != 0) {
				t.Fatal("receipt not immutable")
			}
			data, err := os.ReadFile(ledger + ".payload")
			if err != nil {
				t.Fatal(err)
			}
			var captured struct {
				Args  []string
				Input string
			}
			if json.Unmarshal(data, &captured) != nil || captured.Input != prompt {
				t.Fatal("stdin bytes changed")
			}
			sends := 0
			for _, call := range readHandleCalls(t, ledger) {
				if len(call.Args) > 1 && call.Args[0] == "session" && call.Args[1] == "send" {
					sends++
				}
				for _, arg := range call.Args {
					if arg == "--wait" || arg == "--timeout" || strings.Contains(arg, "private prompt") {
						t.Fatalf("hidden wait or prompt argv: %v", call.Args)
					}
				}
			}
			if sends != 1 || options.IdempotencyKey != "persisted-key" || options.Mode != "plan" {
				t.Fatal("mutation retried or options changed")
			}
		})
	}
	session, ledger := processSendSession(t, "send-normal")
	turn, err := session.Send(t.Context(), "prompt", nil)
	if err != nil || turn.Ref().IdempotencyKey == "" {
		t.Fatalf("nil options key=%v err=%v", turn, err)
	}
	captured, err := os.ReadFile(ledger + ".payload")
	if err != nil || !strings.Contains(string(captured), turn.Ref().IdempotencyKey) {
		t.Fatal("generated key was not original submitted key")
	}
}

func TestProcessSendUncertaintyAndCorrelation(t *testing.T) {
	publicGuard(t)
	for _, mode := range []string{"send-unknown", "send-unknown-number", "send-foreign-key", "send-foreign-ref", "send-foreign-session", "send-valid-then-foreign", "send-partial", "send-missing", "send-error-known-id", "send-model-refusal", "send-note", "send-old", "send-no-cap", "send-no-model"} {
		t.Run(mode, func(t *testing.T) {
			session, ledger := processSendSession(t, mode)
			turn, err := session.Send(t.Context(), "prompt", &SendOptions{IdempotencyKey: "original-key", ClientRef: "caller-ref", Model: "requested-model"})
			var failure *Error
			if !errors.As(err, &failure) || failure.IdempotencyKey != "original-key" || failure.ClientRef != "caller-ref" {
				t.Fatalf("failure/key=%+v", failure)
			}
			definite := mode == "send-note" || mode == "send-old" || mode == "send-no-cap" || mode == "send-no-model"
			if definite {
				if turn != nil || failure.AcceptanceIndeterminate {
					t.Fatalf("preacceptance failure claimed turn=%v err=%+v", turn, failure)
				}
			} else if failure.Code != "indeterminate_acceptance" || !failure.AcceptanceIndeterminate {
				t.Fatalf("uncertainty lost=%+v", failure)
			}
			foreign := mode == "send-foreign-key" || mode == "send-foreign-ref" || mode == "send-foreign-session"
			unknown := mode == "send-unknown" || mode == "send-unknown-number"
			if foreign && (turn != nil || failure.TurnID != "") {
				t.Fatalf("foreign correlation promoted identity: %v %+v", turn, failure)
			}
			if unknown {
				if turn == nil || turn.Ref().TurnID != "turn" || turn.Acceptance() != nil || len(failure.RawRecord) == 0 {
					t.Fatalf("unknown status lost structural identity: %v %+v", turn, failure)
				}
				state, err := turn.State(t.Context())
				if err != nil || state.Ref.TurnID != "turn" {
					t.Fatalf("partial handle cannot inspect original: %v %v", state, err)
				}
			}
			if mode == "send-valid-then-foreign" || mode == "send-partial" {
				if turn == nil || turn.Ref().TurnID != "turn" || turn.Acceptance() == nil {
					t.Fatalf("known receipt overwritten: %v %+v", turn, failure)
				}
			}
			sends := 0
			for _, call := range readHandleCalls(t, ledger) {
				if len(call.Args) > 1 && call.Args[0] == "session" && call.Args[1] == "send" {
					sends++
				}
			}
			expected := 1
			if mode == "send-old" || mode == "send-no-cap" || mode == "send-no-model" {
				expected = 0
			}
			if sends != expected {
				t.Fatalf("retry/preflight violation: sends%d want%d", sends, expected)
			}
		})
	}
}

func TestProcessSendLocalFileAndFIFOValidationBeforeChild(t *testing.T) {
	publicGuard(t)
	session, ledger := processSendSession(t, "send-normal")
	root := t.TempDir()
	invalid := filepath.Join(root, "invalid")
	if err := os.WriteFile(invalid, []byte("unsupported file"), 0600); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(root, "pipe")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	finished := make(chan error, 1)
	go func() { _, err := session.Send(ctx, "prompt", &SendOptions{Files: []string{fifo}}); finished <- err }()
	select {
	case err := <-finished:
		var failure *Error
		if !errors.As(err, &failure) || failure.Code != "invalid_input" || failure.IdempotencyKey == "" {
			t.Fatalf("FIFO result=%v", err)
		}
	case <-ctx.Done():
		writer, _ := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0)
		if writer != nil {
			_ = writer.Close()
		}
		t.Fatal("FIFO preflight blocked opening a special file")
	}
	png := filepath.Join(root, "small.png")
	if err := os.WriteFile(png, []byte("\x89PNG\r\n\x1a\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, options := range []*SendOptions{{Files: []string{invalid}}, {Files: []string{root}}, {Files: make([]string, 9)}, {Mode: "stop"}} {
		_, err := session.Send(t.Context(), "prompt", options)
		var failure *Error
		if !errors.As(err, &failure) || failure.Code != "invalid_input" || failure.IdempotencyKey == "" || failure.AcceptanceIndeterminate {
			t.Fatalf("local invalid=%v", err)
		}
	}
	if _, err := session.Send(t.Context(), strings.Repeat("x", maxSendBytes), &SendOptions{Files: []string{png}}); err == nil {
		t.Fatal("inline combined overflow accepted")
	}
	if _, err := os.Stat(ledger); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("local rejection spawned child: %v", err)
	}
	blob := filepath.Join(root, "large.png")
	file, err := os.OpenFile(blob, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("\x89PNG\r\n\x1a\n"))
	if err := file.Truncate(maxInlineFileBytes + 1); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	// This pins preflight accounting, not actual image decoding/provider support.
	if err := validateSendFiles(t.Context(), strings.Repeat("x", maxSendBytes), []string{blob}, ""); err != nil {
		t.Fatalf("digest-backed file incorrectly charged inline: %v", err)
	}
}

func TestProcessSendCancellationRetainsKeyAndReceipt(t *testing.T) {
	publicGuard(t)
	for _, mode := range []string{"send-cancel-after-receipt", "send-terminal-then-limit"} {
		t.Run(mode, func(t *testing.T) {
			session, ledger := processSendSession(t, mode, WithLimits(Limits{MaxRecordBytes: 512}))
			deadline, stop := context.WithTimeout(t.Context(), 10*time.Second)
			defer stop()
			ctx, cancel := context.WithCancel(deadline)
			defer cancel()
			type result struct {
				turn *Turn
				err  error
			}
			finished := make(chan result, 1)
			go func() { turn, err := session.Send(ctx, "prompt", nil); finished <- result{turn, err} }()
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for {
				if _, err := os.Stat(ledger + ".ready"); err == nil {
					break
				}
				select {
				case result := <-finished:
					t.Fatalf("child exited before barrier: %v", result.err)
				case <-deadline.Done():
					t.Fatal(deadline.Err())
				case <-ticker.C:
				}
			}
			cancel()
			select {
			case result := <-finished:
				var failure *Error
				if result.turn == nil || result.turn.Ref().IdempotencyKey == "" || result.turn.Acceptance() == nil || !errors.As(result.err, &failure) || failure.Code != "indeterminate_acceptance" || failure.IdempotencyKey != result.turn.Ref().IdempotencyKey || !errors.Is(result.err, context.Canceled) {
					t.Fatalf("canceled receipt/key lost: %v %+v", result.turn, failure)
				}
				if mode == "send-terminal-then-limit" && !errors.Is(result.err, execclient.ErrOutputLimit) {
					t.Fatal("independent trailing limit cause masked")
				}
			case <-deadline.Done():
				t.Fatal(deadline.Err())
			}
		})
	}
}

func TestProcessSendValidatedErrorVersusWireFailure(t *testing.T) {
	publicGuard(t)
	for _, mode := range []string{"send-wire-version", "send-unknown-error", "send-invalid-error-scope", "send-invalid-error", "send-explicit-uncertainty"} {
		t.Run(mode, func(t *testing.T) {
			session, _ := processSendSession(t, mode)
			turn, err := session.Send(t.Context(), "prompt", &SendOptions{IdempotencyKey: "original"})
			var failure *Error
			if !errors.As(err, &failure) || turn != nil || failure.IdempotencyKey != "original" {
				t.Fatalf("error/correlation=%v %+v", turn, failure)
			}
			switch mode {
			case "send-invalid-error":
				if failure.Code != "invalid_input" || failure.AcceptanceIndeterminate {
					t.Fatalf("validated pre-submission error=%+v", failure)
				}
			case "send-unknown-error":
				if failure.Code != "future_policy_code" || !failure.AcceptanceIndeterminate || len(failure.RawRecord) == 0 {
					t.Fatalf("unknown original code discarded=%+v", failure)
				}
			default:
				if failure.Code != "indeterminate_acceptance" || !failure.AcceptanceIndeterminate {
					t.Fatalf("unvalidated error declared definitive=%+v", failure)
				}
			}
		})
	}
}

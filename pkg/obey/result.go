package obey

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/obediencecorp/obey-go-sdk/internal/execclient"
	"github.com/obediencecorp/obey-go-sdk/internal/protocol"
)

type resultBuilder struct {
	failedRaw json.RawMessage
	event     Event
	decoder   protocol.Decoder
	result    Result
	maxBytes  int64
}

func newResultBuilder(name string, version int, maxBytes int64) *resultBuilder {
	if maxBytes == 0 {
		maxBytes = execclient.DefaultBytes
	}
	return &resultBuilder{decoder: protocol.Decoder{Protocol: name, Version: version}, result: Result{Protocol: ProtocolVersion{Name: name, Version: version}, Cleanup: CleanupUnknown}, maxBytes: maxBytes}
}
func (b *resultBuilder) add(data []byte) (protocol.Envelope, error) {
	var event Event
	r, err := b.decoder.AddWithValidation(data, func(record protocol.Envelope) (int64, error) {
		var err error
		event, err = projectRecord(record)
		if err != nil {
			return 0, err
		}
		if record.Type == "event" && event.Diagnostic != nil {
			return int64(len(record.Raw)), nil
		}
		return 0, nil
	})
	if err != nil {
		b.failedRaw = append(json.RawMessage(nil), data...)
		return r, err
	}
	b.event = event
	b.result.Session = SessionRef{CampID: b.decoder.CampID, SessionID: b.decoder.SessionID}
	b.result.TurnID = b.decoder.TurnID
	switch r.Type {
	case "assistant_content":
		if r.Content != nil {
			if int64(len(*r.Content)) > b.maxBytes {
				return r, &protocol.Error{Code: "output_limit"}
			}
			b.result.Text = *r.Content
		}
	case "usage":
		if r.InputTokens != nil {
			b.result.Usage.InputTokens = *r.InputTokens
		}
		if r.OutputTokens != nil {
			b.result.Usage.OutputTokens = *r.OutputTokens
		}
		if r.CachedTokens != nil {
			b.result.Usage.CachedTokens = *r.CachedTokens
		}
		if r.LatencyMS != nil {
			b.result.Usage.Latency = time.Duration(*r.LatencyMS) * time.Millisecond
		}
	case "event":
		if event.Completion != nil {
			if int64(len(event.Completion.Text)) > b.maxBytes {
				return r, &protocol.Error{Code: "output_limit"}
			}
			b.result.Text = event.Completion.Text
			b.result.Usage = cloneUsage(event.Completion.Usage)
		}
		if event.Diagnostic != nil {
			b.result.Diagnostics = append(b.result.Diagnostics, cloneDiagnostic(*event.Diagnostic))
		}
	case "error":
		if r.Severity == "" {
			r.Severity = "error"
			if r.Code == "stop_session" {
				r.Severity = "warning"
			}
		}
		b.result.Diagnostics = append(b.result.Diagnostics, Diagnostic{Code: r.Code, Message: r.Message, Severity: r.Severity, DaemonCode: r.DaemonCode, Details: append(json.RawMessage(nil), r.Details...)})
	case "terminal":
		if r.Cleanup != "" {
			b.result.Cleanup = CleanupDisposition(r.Cleanup)
		} else if b.decoder.Created && r.SessionID == b.decoder.SessionID {
			b.result.Cleanup = CleanupStopped
			for _, d := range b.result.Diagnostics {
				if d.Code == "stop_session" {
					b.result.Cleanup = CleanupStopFailed
				}
			}
		}
	}
	return r, nil
}
func (b *resultBuilder) finish(out execclient.Outcome, parseErr error) (*Result, error) {
	err := parseErr
	if err == nil {
		err = b.decoder.Finish(out.ExitCode)
	}
	if err == nil && (out.Err == nil || (out.ExitCode == 0 && contextOnly(out.Err))) {
		return &b.result, nil
	}
	code := "protocol_error"
	var public *Error
	if errors.As(err, &public) {
		code = public.Code
	}
	var pe *protocol.Error
	if errors.As(err, &pe) {
		code = pe.Code
	}
	if code == "canceled" && errors.Is(out.Err, context.DeadlineExceeded) {
		code = "timeout"
	}
	if parseErr == nil && !b.decoder.Terminal {
		if errors.Is(out.Err, context.DeadlineExceeded) {
			code = "timeout"
		} else if errors.Is(out.Err, context.Canceled) {
			code = "canceled"
		}
	}
	if errors.Is(out.Err, execclient.ErrOutputLimit) {
		code = "output_limit"
	}
	resultErr := &Error{Code: code, IdempotencyKey: b.decoder.IdempotencyKey, ClientRef: b.decoder.ClientRef, Operation: b.decoder.Protocol, Protocol: b.result.Protocol, Session: b.result.Session, TurnID: b.result.TurnID, ExitCode: &out.ExitCode, StderrTail: out.StderrTail, Diagnostics: append([]Diagnostic(nil), b.result.Diagnostics...), Cause: errors.Join(err, out.Err)}
	if pe != nil && pe.Record != nil {
		r := pe.Record
		resultErr.RawRecord = append(json.RawMessage(nil), r.Raw...)
		resultErr.RequestedSessionID = r.RequestedSessionID
		resultErr.ObservedSessionID = r.ObservedSessionID
		resultErr.TargetTurnID = r.TargetTurnID
		resultErr.CallID = r.CallID
		resultErr.DecisionIndeterminate = r.DecisionIndeterminate
		resultErr.DaemonCode = r.DaemonCode
		resultErr.GRPCCode = r.GRPCCode
		resultErr.Details = append(json.RawMessage(nil), r.Details...)
		if r.IdempotencyKey != "" {
			resultErr.IdempotencyKey = r.IdempotencyKey
		}
		if r.ClientRef != "" {
			resultErr.ClientRef = r.ClientRef
		}
		resultErr.AcceptanceIndeterminate = r.AcceptanceIndeterminate
	}
	if code == "timeout" {
		resultErr.Cause = errors.Join(resultErr.Cause, context.DeadlineExceeded)
	}
	if code == "canceled" {
		resultErr.Cause = errors.Join(resultErr.Cause, context.Canceled)
	}
	if len(b.failedRaw) > 0 {
		resultErr.RawRecord = append(json.RawMessage(nil), b.failedRaw...)
	}
	if code == "indeterminate_acceptance" {
		resultErr.AcceptanceIndeterminate = true
	}
	return &b.result, resultErr
}

// Definitive completion may supersede only cancellation itself, never an
// independent read/limit/process failure joined to that cancellation.
func contextOnly(err error) bool {
	if err == context.Canceled || err == context.DeadlineExceeded {
		return true
	}
	if many, ok := err.(interface{ Unwrap() []error }); ok {
		children := many.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !contextOnly(child) {
				return false
			}
		}
		return true
	}
	if one, ok := err.(interface{ Unwrap() error }); ok {
		return contextOnly(one.Unwrap())
	}
	return false
}

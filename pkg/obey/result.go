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
	decoder  protocol.Decoder
	result   Result
	maxBytes int64
}

func newResultBuilder(name string, version int, maxBytes int64) *resultBuilder {
	if maxBytes == 0 {
		maxBytes = execclient.DefaultBytes
	}
	return &resultBuilder{decoder: protocol.Decoder{Protocol: name, Version: version}, result: Result{Protocol: ProtocolVersion{Name: name, Version: version}, Cleanup: CleanupUnknown}, maxBytes: maxBytes}
}
func (b *resultBuilder) add(data []byte) (protocol.Envelope, error) {
	r, err := b.decoder.Add(data)
	if err != nil {
		return r, err
	}
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
		if r.PayloadType == "local.v1.SendMessageResponse" {
			var wrapper struct {
				Event struct {
					Type       json.RawMessage `json:"type"`
					Completion *struct {
						Content    string            `json:"content"`
						Model      string            `json:"model"`
						StopReason string            `json:"stop_reason"`
						Input      protocol.Integer  `json:"tokens_input"`
						Output     protocol.Integer  `json:"tokens_output"`
						Cached     protocol.Integer  `json:"cached_tokens"`
						Latency    protocol.Integer  `json:"latency_ms"`
						Cost       *protocol.Integer `json:"total_cost_usd_ticks"`
						Partial    *bool             `json:"cost_is_partial"`
					} `json:"completion"`
				} `json:"event"`
			}
			if err := json.Unmarshal(r.Payload, &wrapper); err != nil {
				return r, &protocol.Error{Code: "protocol_error", Cause: err}
			}
			c := wrapper.Event.Completion
			if c != nil && string(wrapper.Event.Type) == `"AGENT_COMPLETION"` {
				if int64(len(c.Content)) > b.maxBytes {
					return r, &protocol.Error{Code: "output_limit"}
				}
				b.result.Text = c.Content
				b.result.Usage = Usage{InputTokens: int64(c.Input), OutputTokens: int64(c.Output), CachedTokens: int64(c.Cached), Latency: time.Duration(c.Latency) * time.Millisecond, CostIsPartial: c.Partial}
				if c.Cost != nil {
					v := int64(*c.Cost)
					b.result.Usage.CostUSDTicks = &v
				}
			}
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

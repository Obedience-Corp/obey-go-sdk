package obey

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/Obedience-Corp/obey-go-sdk/internal/execclient"
	"github.com/Obedience-Corp/obey-go-sdk/internal/protocol"
)

const unaryTimeout = 30 * time.Second

func (c *Client) requireProtocol(ctx context.Context, key, name string, version int) error {
	caps, err := c.Capabilities(ctx)
	if err != nil {
		return err
	}
	if supports(caps, key, name, version) {
		return nil
	}
	// This retries discovery, never the requested read or mutation. Binary identity
	// changes also invalidate the normal per-client cache before this point.
	caps, err = c.RefreshCapabilities(ctx)
	if err != nil {
		return err
	}
	if !supports(caps, key, name, version) {
		return &Error{Code: "unsupported_protocol", Operation: key, Protocol: ProtocolVersion{Name: name, Version: version}}
	}
	return nil
}
func (c *Client) invalidateCapabilities() {
	c.cache.mu.Lock()
	c.cache.identity = ""
	c.cache.document = nil
	c.cache.mu.Unlock()
}

// finiteDocument bounds the whole finite JSON document independently of final
// assistant content. The caller is its sole record consumer, including shutdown.
func (c *Client) finiteDocument(ctx context.Context, args []string, stdin io.Reader, operation string) ([]byte, execclient.Outcome, error) {
	bounded, cancel := context.WithTimeout(ctx, unaryTimeout)
	defer cancel()
	p, err := c.start(bounded, args, stdin)
	if err != nil {
		return nil, execclient.Outcome{}, err
	}
	limit := c.config.limits.MaxRecordBytes
	if limit == 0 {
		limit = execclient.DefaultBytes
	}
	var data []byte
	var readFailure error
	for {
		record, err := p.DrainNext()
		if err != nil {
			if err != io.EOF {
				readFailure = errors.Join(readFailure, err)
			}
			break
		}
		if readFailure != nil {
			continue
		}
		if int64(len(data))+int64(len(record))+1 > limit {
			readFailure = execclient.ErrOutputLimit
			p.Cancel(readFailure)
			continue
		}
		data = append(data, record...)
		data = append(data, '\n')
	}
	out := p.Wait()
	cause := errors.Join(readFailure, out.Err)
	if errors.Is(cause, execclient.ErrOutputLimit) || errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) || (cause != nil && !exitOnly(cause)) {
		return data, out, c.invocationError(operation, cause, out)
	}
	return data, out, nil
}
func (c *Client) invocationError(operation string, cause error, out execclient.Outcome) *Error {
	code := "command_error"
	switch {
	case errors.Is(cause, execclient.ErrOutputLimit):
		code = "output_limit"
	case errors.Is(cause, context.DeadlineExceeded):
		code = "timeout"
	case errors.Is(cause, context.Canceled):
		code = "canceled"
	}
	if code == "command_error" {
		c.invalidateCapabilities()
	}
	return &Error{Code: code, Operation: operation, ExitCode: ptrCopy(&out.ExitCode), StderrTail: out.StderrTail, Cause: cause}
}
func (c *Client) readJSON(ctx context.Context, args []string, operation string, ref TurnRef) ([]byte, error) {
	if err := c.requireProtocol(ctx, "machine_read_errors", "obey.machine.error", 1); err != nil {
		return nil, scopeError(err, ref)
	}
	args = append(args, "--campaign", ref.CampID, "--json", "--errors-json")
	data, out, err := c.finiteDocument(ctx, args, nil, operation)
	if err != nil {
		return data, scopeError(err, ref)
	}
	var marker struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &marker); err != nil {
		return nil, scopeError(&Error{Code: "protocol_error", Operation: operation, ExitCode: &out.ExitCode, StderrTail: out.StderrTail, Cause: err}, ref)
	}
	if marker.Schema == "obey.machine.error" {
		doc, err := protocol.DecodeReadError(data, operation)
		if err != nil {
			return nil, scopeError(&Error{Code: "protocol_error", Operation: operation, Cause: err}, ref)
		}
		if out.ExitCode == 0 {
			return nil, scopeError(&Error{Code: "protocol_error", Operation: operation, Cause: errors.New("read error document with successful process")}, ref)
		}
		e := &Error{Code: doc.Code, Operation: operation, Protocol: ProtocolVersion{Name: doc.Schema, Version: doc.Version}, Session: ref.SessionRef, TurnID: ref.TurnID, IdempotencyKey: ref.IdempotencyKey, ClientRef: ref.ClientRef, DaemonCode: ptrCopy(doc.DaemonCode), GRPCCode: doc.GRPCCode, ExitCode: &out.ExitCode, StderrTail: out.StderrTail, Details: append(json.RawMessage(nil), doc.Details...), Cause: out.Err}
		if doc.CampID != ref.CampID || (doc.SessionID != "" && doc.SessionID != ref.SessionID) || (doc.TurnID != "" && doc.TurnID != ref.TurnID) {
			e.Code = "scope_mismatch"
		}
		e.RawRecord = append(json.RawMessage(nil), data...)
		e.Diagnostics = []Diagnostic{{Code: doc.Code, Message: doc.Message, Severity: "error", DaemonCode: ptrCopy(doc.DaemonCode), Details: append(json.RawMessage(nil), doc.Details...)}}
		e.Cause = errors.Join(out.Err, errors.New(doc.Message))
		switch e.Code {
		case "canceled":
			e.Cause = errors.Join(e.Cause, context.Canceled)
		case "timeout":
			e.Cause = errors.Join(e.Cause, context.DeadlineExceeded)
		}
		if e.Code == "unsupported_capability" || e.Code == "daemon_unavailable" {
			c.invalidateCapabilities()
		}
		return nil, e
	}
	if out.ExitCode != 0 {
		return nil, scopeError(c.invocationError(operation, out.Err, out), ref)
	}
	return data, nil
}
func scopeError(err error, ref TurnRef) error {
	if err == nil {
		return nil
	}
	var typed *Error
	if !errors.As(err, &typed) {
		return &Error{Code: "command_error", Session: ref.SessionRef, TurnID: ref.TurnID, IdempotencyKey: ref.IdempotencyKey, ClientRef: ref.ClientRef, Cause: err}
	}
	copy := *typed
	copy.Session = ref.SessionRef
	copy.TurnID = ref.TurnID
	copy.IdempotencyKey = ref.IdempotencyKey
	copy.ClientRef = ref.ClientRef
	return &copy
}

func (c *Client) machineDocument(ctx context.Context, args []string, operation, name string, ref TurnRef, expectedCall ...string) ([]protocol.Envelope, error) {
	return c.machineDocumentInput(ctx, args, nil, operation, name, ref, expectedCall...)
}
func (c *Client) machineDocumentInput(ctx context.Context, args []string, stdin io.Reader, operation, name string, ref TurnRef, expectedCall ...string) ([]protocol.Envelope, error) {
	bounded, cancel := context.WithTimeout(ctx, unaryTimeout)
	defer cancel()
	process, err := c.start(bounded, args, stdin)
	if err != nil {
		return nil, scopeError(err, ref)
	}
	decoder := protocol.Decoder{Protocol: name, Version: 1, CampID: ref.CampID, SessionID: ref.SessionID, TurnID: ref.TurnID, IdempotencyKey: ref.IdempotencyKey, ClientRef: ref.ClientRef}
	if name == "obey.session.create" && !fullUUID(ref.CampID) {
		decoder.CampID = ""
	}
	var records []protocol.Envelope
	var parseErr, drainErr error
	var invalidRaw json.RawMessage
	retained := map[string]bool{}
	for {
		raw, err := process.DrainNext()
		if err != nil {
			if err != io.EOF {
				drainErr = err
			}
			break
		}
		if parseErr != nil {
			continue
		} // Still drain shutdown, with one sole consumer.
		record, err := decoder.AddWithValidation(raw, func(record protocol.Envelope) (int64, error) {
			if len(expectedCall) != 0 && record.CallID != expectedCall[0] {
				return 0, &protocol.Error{Code: "scope_mismatch", Cause: errors.New("approval frame lacks the exact requested call ID")}
			}
			return 0, nil
		})
		if err != nil {
			parseErr = err
			invalidRaw = append(json.RawMessage(nil), raw...)
			process.Cancel(err)
			continue
		}
		key := ""
		switch record.Type {
		case "session_created", "terminal":
			key = record.Type
		case "event":
			switch record.PayloadType {
			case "local.v1.PingResponse", "local.v1.RespondApprovalResponse", "local.v1.StopSessionResponse", "local.v1.CancelTurnResponse", "local.v1.SendMessageResponse":
				key = record.PayloadType
			}
		}
		if key != "" {
			if retained[key] {
				parseErr = &protocol.Error{Code: "protocol_error", Cause: errors.New("duplicate finite response")}
				invalidRaw = append(json.RawMessage(nil), raw...)
				process.Cancel(parseErr)
				continue
			}
			retained[key] = true
			records = append(records, record)
		}
	}
	out := process.Wait()
	out.Err = errors.Join(drainErr, out.Err)
	if decoder.SessionID != "" {
		ref.SessionID = decoder.SessionID
	}
	if decoder.CampID != "" {
		ref.CampID = decoder.CampID
	}
	if decoder.TurnID != "" {
		ref.TurnID = decoder.TurnID
	}
	finishErr := parseErr
	if finishErr == nil {
		finishErr = decoder.Finish(out.ExitCode)
	}
	if finishErr == nil && (out.Err == nil || (out.ExitCode == 0 && contextOnly(out.Err))) {
		return records, nil
	}
	if errors.Is(out.Err, execclient.ErrOutputLimit) {
		return records, scopeError(c.invocationError(operation, out.Err, out), ref)
	}
	if finishErr != nil {
		resultErr := protocolCallError(operation, name, ref, finishErr, out)
		if len(invalidRaw) > 0 {
			resultErr.RawRecord = invalidRaw
			resultErr.Diagnostics = append(resultErr.Diagnostics, Diagnostic{Code: resultErr.Code, Message: finishErr.Error(), Severity: "error", Details: append(json.RawMessage(nil), invalidRaw...)})
		}
		return records, resultErr
	}
	return records, scopeError(c.invocationError(operation, out.Err, out), ref)
}

func protocolCallError(operation, name string, ref TurnRef, cause error, out execclient.Outcome) *Error {
	e := &Error{Code: "protocol_error", Operation: operation, Protocol: ProtocolVersion{Name: name, Version: 1}, Session: ref.SessionRef, TurnID: ref.TurnID, IdempotencyKey: ref.IdempotencyKey, ClientRef: ref.ClientRef, ExitCode: ptrCopy(&out.ExitCode), StderrTail: out.StderrTail, Cause: errors.Join(cause, out.Err)}
	var pe *protocol.Error
	if errors.As(cause, &pe) {
		e.Code = pe.Code
		if record := pe.Record; record != nil {
			e.validatedCLIRecord = true
			e.DaemonCode = ptrCopy(record.DaemonCode)
			e.GRPCCode = record.GRPCCode
			e.Details = append(json.RawMessage(nil), record.Details...)
			e.RawRecord = append(json.RawMessage(nil), record.Raw...)
			e.AcceptanceIndeterminate = record.AcceptanceIndeterminate
			e.RequestedSessionID = record.RequestedSessionID
			e.ObservedSessionID = record.ObservedSessionID
			e.TargetTurnID = record.TargetTurnID
			e.CallID = record.CallID
			e.DecisionIndeterminate = record.DecisionIndeterminate
			e.Diagnostics = []Diagnostic{{Code: record.Code, Message: record.Message, Severity: record.Severity, DaemonCode: ptrCopy(record.DaemonCode), Details: append(json.RawMessage(nil), record.Details...)}}
		}
	}
	if e.Code == "canceled" && errors.Is(out.Err, context.DeadlineExceeded) {
		e.Code = "timeout"
	}
	switch e.Code {
	case "canceled":
		e.Cause = errors.Join(e.Cause, context.Canceled)
	case "timeout":
		e.Cause = errors.Join(e.Cause, context.DeadlineExceeded)
	}
	return e
}

func fullUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, c := range value {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}

package obey

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/obediencecorp/obey-go-sdk/internal/execclient"
	"github.com/obediencecorp/obey-go-sdk/internal/protocol"
	"io"
	"time"
)

const turnPollInterval = 250 * time.Millisecond

// Wait observes this accepted turn. Only the caller context bounds its lifetime;
// each read/stream establishment remains bounded independently.
func (t *Turn) Wait(ctx context.Context) (*Result, error) {
	if err := t.client.checkOpen(); err != nil {
		return nil, err
	}
	result := &Result{Session: t.ref.SessionRef, TurnID: t.ref.TurnID, Cleanup: CleanupNotOwned, RequestedSessionID: t.ref.SessionID, ObservedSessionID: t.ref.SessionID}
	if err := validateTurnRef(t.ref); err != nil {
		return result, scopeError(&Error{Code: "invalid_input", Operation: "turn.wait", Cause: err}, t.ref)
	}
	done, err := t.resolveWait(ctx, result)
	if done || err != nil {
		return result, scopeError(err, t.ref)
	}
	err = t.watchWait(ctx, result)
	return result, scopeError(err, t.ref)
}
func (t *Turn) waitError(code, message string) error {
	return scopeError(&Error{Code: code, Operation: "turn.wait", Cause: errors.New(message)}, t.ref)
}
func (t *Turn) resolveWait(ctx context.Context, result *Result) (bool, error) {
	state, err := t.State(ctx)
	if err != nil {
		return false, err
	}
	result.Usage = state.Usage
	switch state.Status {
	case TurnQueued, TurnRunning:
		return false, nil
	case TurnCompleted:
		err := t.retainedResult(ctx, state, result)
		return true, t.withWaitState(err, state)
	case TurnFailed:
		return true, t.withWaitState(t.waitError("daemon_error", "accepted target turn failed"), state)
	case TurnCanceled:
		return true, t.withWaitState(t.waitError("canceled", "accepted target turn was canceled"), state)
	case TurnRecoveryRequired:
		return true, t.withWaitState(t.waitError("recovery_required", "accepted target turn requires explicit recovery"), state)
	case TurnHistoricalUnknown:
		return true, t.withWaitState(t.waitError("result_unavailable", "historical turn result is unknown"), state)
	default:
		return true, t.withWaitState(t.waitError("protocol_error", "unknown target turn status"), state)
	}
}
func (t *Turn) withWaitState(err error, state *TurnState) error {
	if err == nil {
		return nil
	}
	scoped := scopeError(err, t.ref)
	var e *Error
	if errors.As(scoped, &e) {
		copy := *e
		if len(copy.RawRecord) == 0 {
			copy.RawRecord = append(json.RawMessage(nil), state.Raw...)
		}
		if state.ErrorCode != "" || state.ErrorMessage != "" {
			copy.Diagnostics = append(cloneDiagnostics(copy.Diagnostics), Diagnostic{Code: state.ErrorCode, Message: state.ErrorMessage, Severity: "error"})
		}
		return &copy
	}
	return scoped
}
func (t *Turn) completedAttempt(state *TurnState) (string, error) {
	doc, err := protocol.DecodeTurn(state.Raw)
	if err != nil {
		return "", t.waitError("protocol_error", "invalid completed state")
	}
	a := doc.Turn.LatestAttempt
	if a == nil || a.ID == "" {
		return "", t.waitError("result_unavailable", "completed target lacks latest attempt identity")
	}
	if a.TurnID != t.ref.TurnID || (a.State != "completed" && a.State != "TURN_ATTEMPT_COMPLETED") || a.Number < 1 {
		return "", t.waitError("protocol_error", "completed target latest attempt is inconsistent")
	}
	return a.ID, nil
}
func (t *Turn) retainedResult(ctx context.Context, state *TurnState, result *Result) error {
	attempt, err := t.completedAttempt(state)
	if err != nil {
		return err
	}
	session := &Session{client: t.client, ref: t.ref.SessionRef}
	cut, err := session.transcript(ctx, TranscriptOptions{PageSize: 1}, true)
	if err != nil {
		return err
	}
	if len(cut.Entries) != 1 || cut.Entries[0].Sequence == 0 {
		return t.waitError("result_unavailable", "completed target transcript is not retained")
	}
	high := cut.Entries[0].Sequence
	result.HighWaterSequence = ptrCopy(&high)
	result.OutputSource = "retained_transcript"
	result.OutputFidelity = string(FidelityFinalOnly)
	var previous uint64
	cursor := ""
	reached := false
	found := false
	for {
		page, err := session.transcript(ctx, TranscriptOptions{PageSize: 500, Cursor: cursor}, false)
		if err != nil {
			return err
		}
		for _, entry := range page.Entries {
			if entry.Sequence == 0 || entry.Sequence <= previous {
				return t.waitError("protocol_error", "retained transcript sequence did not advance")
			}
			previous = entry.Sequence
			if entry.Sequence > high {
				break
			}
			if entry.Sequence == high {
				reached = true
			}
			if entry.TurnID == t.ref.TurnID && entry.AttemptID == attempt && entry.Kind == "completion" && entry.ActivityType == "AGENT_COMPLETION" {
				limit := t.client.config.limits.MaxResultBytes
				if limit == 0 {
					limit = execclient.DefaultBytes
				}
				if int64(len(entry.Content)) > limit {
					return t.waitError("output_limit", "retained final content exceeds configured limit")
				}
				result.Text = entry.Content
				found = true
			}
		}
		if reached || previous > high || !page.HasMore {
			break
		}
		if len(page.Entries) == 0 || page.NextCursor == "" || page.NextCursor == cursor {
			return t.waitError("protocol_error", "retained transcript cursor did not advance")
		}
		cursor = page.NextCursor
	}
	if !reached {
		return t.waitError("result_unavailable", "captured transcript boundary disappeared")
	}
	if !found {
		return t.waitError("result_unavailable", "latest successful attempt completion is not retained")
	}
	latest, err := t.State(ctx)
	if err != nil {
		return err
	}
	latestAttempt, err := t.completedAttempt(latest)
	if err != nil {
		return err
	}
	if latest.Status != TurnCompleted || latestAttempt != attempt {
		return t.waitError("result_unavailable", "target latest attempt changed while reading retained result")
	}
	result.Usage = latest.Usage
	return nil
}

type waitRecord struct {
	data []byte
	err  error
}

// The pump is always the sole process record consumer. Once canceled it drains
// without forwarding, so backpressure cannot prevent child detachment/reaping.
func pumpWait(ctx context.Context, p *execclient.Process, records chan<- waitRecord, done chan<- struct{}) {
	defer close(done)
	for {
		data, err := p.DrainNext()
		select {
		case records <- waitRecord{data, err}:
		case <-ctx.Done():
		}
		if err != nil {
			return
		}
	}
}
func (t *Turn) watchWait(ctx context.Context, result *Result) (waitErr error) {
	if err := t.client.requireProtocol(ctx, "session_attach", "obey.session.attach", 1); err != nil {
		return err
	}
	watchCtx, cancel := context.WithCancelCause(ctx)
	p, err := t.client.start(watchCtx, []string{"session", "attach", "id:" + t.ref.SessionID, "--campaign", t.ref.CampID, "--json", "--protocol-version", "1", "--backfill-limit", "500"}, nil)
	if err != nil {
		cancel(nil)
		return err
	}
	timer := time.AfterFunc(unaryTimeout, func() { cancel(context.DeadlineExceeded) })
	defer timer.Stop()
	records := make(chan waitRecord, 1)
	done := make(chan struct{})
	go pumpWait(watchCtx, p, records, done)
	defer func() {
		cancel(nil)
		<-done
		out := p.Wait()
		if errors.Is(out.Err, execclient.ErrOutputLimit) {
			if waitErr == nil {
				waitErr = t.client.invocationError("turn.wait", out.Err, out)
			} else {
				var primary *Error
				if errors.As(waitErr, &primary) {
					copy := *primary
					copy.Cause = errors.Join(copy.Cause, out.Err)
					copy.ExitCode = ptrCopy(&out.ExitCode)
					copy.StderrTail = out.StderrTail
					waitErr = &copy
				} else {
					waitErr = errors.Join(waitErr, out.Err)
				}
			}
		}
	}()
	decoder := protocol.Decoder{Protocol: "obey.session.attach", Version: 1}
	ticker := time.NewTicker(turnPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-watchCtx.Done():
			out := p.Wait()
			return t.client.invocationError("turn.wait", errors.Join(context.Cause(watchCtx), out.Err), out)
		case <-ticker.C:
			done, err := t.resolveWait(ctx, result)
			if done || err != nil {
				return err
			}
		case item := <-records:
			if item.err != nil {
				done, err := t.resolveWait(ctx, result)
				if done || err != nil {
					return err
				}
				if item.err == io.EOF {
					return t.waitError("incomplete_stream", "attach ended before target completed")
				}
				return t.client.invocationError("turn.wait", item.err, p.Wait())
			}
			record, err := decoder.Add(item.data)
			if err != nil {
				return &Error{Code: "protocol_error", Operation: "turn.wait", RawRecord: append(json.RawMessage(nil), item.data...), Cause: err}
			}
			if record.Type == "terminal" || record.Type == "error" {
				done, readErr := t.resolveWait(ctx, result)
				if done || readErr != nil {
					return readErr
				}
			}
			marker, err := t.waitObservation(record, result)
			if err != nil {
				return err
			}
			if marker {
				timer.Stop()
				done, err := t.resolveWait(ctx, result)
				if done || err != nil {
					return err
				}
			}
		}
	}
}
func (t *Turn) waitObservation(r protocol.Envelope, result *Result) (bool, error) {
	if r.CampID != t.ref.CampID || r.RequestedSessionID != t.ref.SessionID || r.SessionID == "" {
		return false, t.waitError("scope_mismatch", "attach differs from requested original camp/session")
	}
	observed := r.ObservedSessionID
	if observed == "" {
		observed = r.SessionID
	}
	if r.SessionID != observed {
		return false, t.waitError("scope_mismatch", "attach outer session differs from resolved observation")
	}
	result.ObservedSessionID = observed
	if r.Type == "error" {
		return false, &Error{Code: r.Code, Operation: "turn.wait", RawRecord: append(json.RawMessage(nil), r.Raw...), DaemonCode: ptrCopy(r.DaemonCode), GRPCCode: r.GRPCCode, Cause: errors.New(r.Message)}
	}
	if r.Type == "terminal" {
		return false, t.waitError("incomplete_stream", "attach ended before target completed")
	}
	if r.Type != "event" || r.PayloadType != "local.v1.AttachSessionResponse" {
		return false, nil
	}
	var wrapper struct {
		Marker   json.RawMessage `json:"marker"`
		Backfill *struct {
			Session string `json:"session_id"`
			Camp    string `json:"campaign_id"`
			Turn    string `json:"turn_id"`
		} `json:"backfill"`
		Live *struct {
			Session string `json:"session_id"`
			Turn    string `json:"turn_id"`
		} `json:"live"`
	}
	if err := json.Unmarshal(r.Payload, &wrapper); err != nil {
		return false, t.waitError("protocol_error", "invalid attach wrapper")
	}
	if b := wrapper.Backfill; b != nil && (b.Session != observed || b.Camp != t.ref.CampID || b.Turn != r.TurnID) {
		return false, t.waitError("scope_mismatch", "attach backfill identity differs")
	}
	if live := wrapper.Live; live != nil && (live.Session != observed || live.Turn != r.TurnID) {
		return false, t.waitError("scope_mismatch", "attach activity identity differs")
	}
	if len(wrapper.Marker) > 0 && string(wrapper.Marker) != "null" {
		var marker struct {
			Type json.RawMessage `json:"type"`
		}
		if err := json.Unmarshal(wrapper.Marker, &marker); err != nil {
			return false, t.waitError("protocol_error", "invalid attach marker")
		}
		var kind string
		_ = json.Unmarshal(marker.Type, &kind)
		return kind == "ATTACH_MARKER_TYPE_BACKFILL_COMPLETE", nil
	}
	return false, nil
}

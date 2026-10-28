package obey

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/obediencecorp/obey-go-sdk/internal/protocol"
)

type Turn struct {
	client     *Client
	ref        TurnRef
	acceptance *Acceptance
}

func (t *Turn) Ref() TurnRef { return t.ref }
func (t *Turn) Acceptance() *Acceptance {
	if t.acceptance == nil {
		return nil
	}
	copy := *t.acceptance
	copy.QueuePosition = ptrCopy(copy.QueuePosition)
	copy.Raw = append(json.RawMessage(nil), copy.Raw...)
	return &copy
}
func validateSessionRef(ref SessionRef) error {
	if strings.TrimSpace(ref.CampID) == "" || strings.TrimSpace(ref.SessionID) == "" {
		return errors.New("camp and exact session IDs are required")
	}
	return nil
}
func validateTurnRef(ref TurnRef) error {
	if err := validateSessionRef(ref.SessionRef); err != nil {
		return err
	}
	if strings.TrimSpace(ref.TurnID) == "" {
		return errors.New("exact turn ID is required")
	}
	return nil
}
func (c *Client) OpenTurn(ctx context.Context, ref TurnRef) (*Turn, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	if err := validateTurnRef(ref); err != nil {
		return nil, scopeError(&Error{Code: "invalid_input", Operation: "turn.open", Cause: err}, ref)
	}
	turn := &Turn{client: c, ref: ref}
	if _, err := turn.State(ctx); err != nil {
		return nil, err
	}
	return turn, nil
}
func (t *Turn) State(ctx context.Context) (*TurnState, error) {
	data, err := t.client.readJSON(ctx, []string{"turn", "show", t.ref.TurnID}, "turn.show", t.ref)
	if err != nil {
		return nil, err
	}
	state, err := decodeTurnState(data, t.ref)
	if err != nil {
		return nil, scopeError(err, t.ref)
	}
	return state, nil
}
func canonicalTurnStatus(raw string) TurnStatus {
	switch raw {
	case "queued", "TURN_QUEUED":
		return TurnQueued
	case "running", "TURN_RUNNING":
		return TurnRunning
	case "completed", "TURN_COMPLETED":
		return TurnCompleted
	case "failed", "TURN_FAILED":
		return TurnFailed
	case "cancelled", "canceled", "TURN_CANCELLED":
		return TurnCanceled
	case "recovery_required", "TURN_RECOVERY_REQUIRED":
		return TurnRecoveryRequired
	case "historical_unknown", "TURN_HISTORICAL_UNKNOWN":
		return TurnHistoricalUnknown
	default:
		return TurnStatus(raw)
	}
}
func decodeTurnState(data []byte, ref TurnRef) (*TurnState, error) {
	doc, err := protocol.DecodeTurn(data)
	if err != nil {
		return nil, &Error{Code: "protocol_error", Operation: "turn.show", Cause: err}
	}
	turn := doc.Turn
	if turn.ID != ref.TurnID || turn.SessionID != ref.SessionID || turn.CampID != ref.CampID {
		return nil, &Error{Code: "scope_mismatch", Operation: "turn.show", RawRecord: append(json.RawMessage(nil), data...), Cause: errors.New("returned turn scope differs from requested camp/session/turn")}
	}
	state := &TurnState{Ref: ref, Status: canonicalTurnStatus(turn.Status), Version: turn.Version, QueuePosition: ptrCopy(turn.QueuePosition), RequestedProvider: turn.RequestedProvider, RequestedModel: turn.RequestedModel, RequestedMode: turn.RequestedMode, RecoveryRequired: canonicalTurnStatus(turn.Status) == TurnRecoveryRequired, ErrorCode: turn.ErrorCode, ErrorMessage: turn.ErrorMessage, Raw: append(json.RawMessage(nil), data...)}
	if attempt := turn.LatestAttempt; attempt != nil {
		if attempt.TurnID != "" && attempt.TurnID != ref.TurnID {
			return nil, &Error{Code: "scope_mismatch", Operation: "turn.show", Cause: errors.New("latest attempt belongs to another turn")}
		}
		state.LatestAttemptID = attempt.ID
	}
	if turn.Recovery != nil {
		state.RecoveryReason = turn.Recovery.Reason
	}
	if len(turn.Usage) > 0 && string(turn.Usage) != "null" {
		var usage protocol.NumericUsage
		if err := json.Unmarshal(turn.Usage, &usage); err != nil {
			return nil, &Error{Code: "protocol_error", Operation: "turn.show", Cause: err}
		}
		state.Usage = Usage{InputTokens: usage.Input, OutputTokens: usage.Output, CachedTokens: usage.Cached, CostUSDTicks: ptrCopy(usage.Cost), CostIsPartial: ptrCopy(usage.CostPartial)}
		if usage.Incomplete != nil {
			complete := !*usage.Incomplete
			state.Usage.Complete = &complete
		}
	}
	if turn.DurationMS < 0 || turn.DurationMS > int64((1<<63-1)/time.Millisecond) {
		return nil, &Error{Code: "protocol_error", Operation: "turn.show", Cause: errors.New("invalid turn duration")}
	}
	state.Usage.Latency = time.Duration(turn.DurationMS) * time.Millisecond
	return state, nil
}

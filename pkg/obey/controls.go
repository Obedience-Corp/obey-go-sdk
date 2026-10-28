package obey

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/obediencecorp/obey-go-sdk/internal/protocol"
)

// PendingApprovals returns a copied snapshot bound to this exact session. Live
// approval events use the same (SessionRef, CallID) identity; no decision is made.
func (s *Session) PendingApprovals(ctx context.Context) ([]Approval, error) {
	ref := TurnRef{SessionRef: s.ref}
	if err := s.client.checkOpen(); err != nil {
		return nil, err
	}
	if err := validateSessionRef(s.ref); err != nil {
		return nil, controlInput("session.approvals", ref, err)
	}
	data, err := s.client.readJSON(ctx, []string{"session", "approvals", "--session", s.ref.SessionID}, "session.approvals", ref)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Approvals []json.RawMessage `json:"approvals"`
	}
	if err := payloadObject(data, &doc); err != nil {
		return nil, controlPayloadError("session.approvals", ref, data, err)
	}
	var out []Approval
	seen := map[string]bool{}
	for _, raw := range doc.Approvals {
		var scope struct {
			Session string `json:"session_id"`
			Camp    string `json:"campaign_id"`
		}
		if err := payloadObject(raw, &scope); err != nil {
			return nil, controlPayloadError("session.approvals", ref, raw, err)
		}
		if scope.Session != s.ref.SessionID || scope.Camp != s.ref.CampID {
			return nil, scopeError(&Error{Code: "scope_mismatch", Operation: "session.approvals", RawRecord: append(json.RawMessage(nil), raw...), Cause: errors.New("pending approval belongs to another camp/session")}, ref)
		}
		approval, err := pendingApprovalProjection(raw, s.ref)
		if err != nil {
			return nil, controlPayloadError("session.approvals", ref, raw, err)
		}
		if !seen[approval.CallID] {
			out = append(out, approval)
			seen[approval.CallID] = true
		}
	}
	return out, nil
}

// RespondApproval sends one explicit decision. Errors can retain
// DecisionIndeterminate; callers must inspect/reconcile rather than auto-retry.
func (s *Session) RespondApproval(ctx context.Context, decision ApprovalDecision) error {
	ref := TurnRef{SessionRef: s.ref}
	if err := s.client.checkOpen(); err != nil {
		return err
	}
	if err := validateSessionRef(s.ref); err != nil {
		return controlInput("session.approve", ref, err)
	}
	if strings.TrimSpace(decision.CallID) == "" || strings.ContainsRune(decision.CallID, '\x00') || strings.ContainsRune(decision.Reason, '\x00') {
		return controlInput("session.approve", ref, errors.New("exact call ID and NUL-free reason required"))
	}
	if err := s.client.controlPreflight(ctx, "session_approve", "obey.session.approve", ref, "chat_surfaces"); err != nil {
		return err
	}
	args := []string{"session", "approve", decision.CallID, "--session", s.ref.SessionID, "--campaign", s.ref.CampID, "--json", "--protocol-version", "1"}
	if !decision.Approve {
		args = append(args, "--deny")
	}
	if decision.Reason != "" {
		args = append(args, "--reason", decision.Reason)
	}
	records, err := s.client.machineDocument(ctx, args, "session.approve", "obey.session.approve", ref, decision.CallID)
	if err != nil {
		var typed *Error
		if errors.As(err, &typed) {
			copy := *typed
			copy.CallID = decision.CallID
			if typed.CallID != "" && typed.CallID != decision.CallID {
				copy.Code = "scope_mismatch"
				copy.DecisionIndeterminate = true
				return &copy
			}
			// Only a validated CLI refusal can prove a decision was not applied.
			definite := false
			if typed.validatedCLIRecord && !typed.DecisionIndeterminate {
				switch typed.Code {
				case "approval_expired", "approval_resolved", "stale_call", "scope_mismatch", "not_found", "unsupported_capability", "invalid_input":
					definite = true
				}
			}
			if !definite && typed.ExitCode != nil {
				copy.DecisionIndeterminate = true
			}
			return &copy
		}
		return err
	}
	for _, record := range records {
		if record.CallID != "" && record.CallID != decision.CallID {
			return scopeError(&Error{Code: "scope_mismatch", Operation: "session.approve", CallID: decision.CallID, DecisionIndeterminate: true, RawRecord: append(json.RawMessage(nil), record.Raw...), Cause: errors.New("approval response belongs to another call")}, ref)
		}
	}
	raw := controlResponse(records, "local.v1.RespondApprovalResponse")
	var response map[string]json.RawMessage
	if err := payloadObject(raw, &response); err != nil {
		e := controlPayloadError("session.approve", ref, raw, err)
		var typed *Error
		if errors.As(e, &typed) {
			typed.CallID = decision.CallID
			typed.DecisionIndeterminate = true
		}
		return e
	}
	return nil
}

// Cancel explicitly targets this durable turn. Its acknowledgement may precede
// physical provider exit; post-dispatch ambiguity can remain recovery_required.
func (t *Turn) Cancel(ctx context.Context) (*CancelResult, error) {
	if err := t.client.checkOpen(); err != nil {
		return nil, err
	}
	if err := validateTurnRef(t.ref); err != nil {
		return nil, controlInput("turn.cancel", t.ref, err)
	}
	if err := t.client.controlPreflight(ctx, "turn_cancel", "obey.turn.cancel", t.ref, "chat_surfaces", "cancel_turn"); err != nil {
		return nil, err
	}
	args := []string{"turn", "cancel", t.ref.TurnID, "--session", t.ref.SessionID, "--campaign", t.ref.CampID, "--json", "--protocol-version", "1"}
	records, err := t.client.machineDocument(ctx, args, "turn.cancel", "obey.turn.cancel", t.ref)
	if err != nil {
		return nil, err
	}
	raw := controlResponse(records, "local.v1.CancelTurnResponse")
	var response struct {
		Status json.RawMessage `json:"status"`
		Turn   json.RawMessage `json:"turn"`
	}
	if err := payloadObject(raw, &response); err != nil {
		return nil, controlPayloadError("turn.cancel", t.ref, raw, err)
	}
	status := canonicalTurnStatus(enumText(response.Status))
	switch status {
	case TurnQueued, TurnRunning, TurnCompleted, TurnFailed, TurnCanceled, TurnRecoveryRequired, TurnHistoricalUnknown:
	default:
		return nil, controlPayloadError("turn.cancel", t.ref, raw, errors.New("unknown cancellation acknowledgement status"))
	}
	result := &CancelResult{Status: status}
	if len(response.Turn) > 0 && string(response.Turn) != "null" {
		state, err := decodeControlTurn(response.Turn, t.ref)
		if err != nil {
			return result, err
		}
		result.State = state
	}
	return result, nil
}

// Stop sends exactly one explicit StopSession RPC. The daemon retains its
// existing managed-child custody cleanup; unrelated sessions are not selected.
func (s *Session) Stop(ctx context.Context) error {
	ref := TurnRef{SessionRef: s.ref}
	if err := s.client.checkOpen(); err != nil {
		return err
	}
	if err := validateSessionRef(s.ref); err != nil {
		return controlInput("session.stop", ref, err)
	}
	if err := s.client.controlPreflight(ctx, "session_stop", "obey.session.stop", ref, "chat_surfaces"); err != nil {
		return err
	}
	args := []string{"session", "stop", s.ref.SessionID, "--campaign", s.ref.CampID, "--json", "--protocol-version", "1"}
	records, err := s.client.machineDocument(ctx, args, "session.stop", "obey.session.stop", ref)
	if err != nil {
		return err
	}
	raw := controlResponse(records, "local.v1.StopSessionResponse")
	var response struct {
		Session struct {
			ID   string `json:"id"`
			Camp string `json:"campaign_id"`
		} `json:"session"`
	}
	if err := payloadObject(raw, &response); err != nil {
		return controlPayloadError("session.stop", ref, raw, err)
	}
	if response.Session.ID != s.ref.SessionID || response.Session.Camp != s.ref.CampID {
		return scopeError(&Error{Code: "scope_mismatch", Operation: "session.stop", RawRecord: append(json.RawMessage(nil), raw...), Cause: errors.New("stop response differs from exact target")}, ref)
	}
	return nil
}
func (c *Client) controlPreflight(ctx context.Context, key, name string, ref TurnRef, features ...string) error {
	if err := c.requireProtocol(ctx, key, name, 1); err != nil {
		return scopeError(err, ref)
	}
	return scopeError(c.requireFeatures(ctx, features...), ref)
}
func controlResponse(records []protocol.Envelope, kind string) json.RawMessage {
	for _, r := range records {
		if r.Type == "event" && r.PayloadType == kind {
			return r.Payload
		}
	}
	return nil
}
func controlInput(operation string, ref TurnRef, err error) error {
	return scopeError(&Error{Code: "invalid_input", Operation: operation, Cause: err}, ref)
}
func controlPayloadError(operation string, ref TurnRef, raw []byte, err error) error {
	return scopeError(&Error{Code: "protocol_error", Operation: operation, RawRecord: append(json.RawMessage(nil), raw...), Cause: err}, ref)
}

// Decode the original protojson snapshot without routing its quoted int64 fields
// through floating point or conflating it with the numeric turn.show DTO.
func decodeControlTurn(raw json.RawMessage, ref TurnRef) (*TurnState, error) {
	var doc struct {
		ID           string           `json:"id"`
		Session      string           `json:"session_id"`
		Camp         string           `json:"campaign_id"`
		Status       json.RawMessage  `json:"status"`
		Version      protocol.Integer `json:"version"`
		Duration     protocol.Integer `json:"duration_ms"`
		Position     *uint32          `json:"queue_position"`
		Provider     string           `json:"requested_provider"`
		Model        string           `json:"requested_model"`
		Mode         string           `json:"requested_mode"`
		ErrorCode    string           `json:"error_code"`
		ErrorMessage string           `json:"error_message"`
		Attempt      *struct {
			ID   string `json:"id"`
			Turn string `json:"turn_id"`
		} `json:"latest_attempt"`
		Recovery *struct {
			Reason string `json:"reason"`
		} `json:"recovery"`
		Usage struct {
			Input      protocol.Integer  `json:"tokens_input"`
			Output     protocol.Integer  `json:"tokens_output"`
			Cached     protocol.Integer  `json:"cached_tokens"`
			Cost       *protocol.Integer `json:"cost_usd_ticks"`
			Partial    *bool             `json:"cost_is_partial"`
			Incomplete *bool             `json:"usage_is_incomplete"`
		} `json:"usage"`
	}
	if err := payloadObject(raw, &doc); err != nil {
		return nil, controlPayloadError("turn.cancel", ref, raw, err)
	}
	if doc.ID != ref.TurnID || doc.Session != ref.SessionID || doc.Camp != ref.CampID || (doc.Attempt != nil && doc.Attempt.Turn != "" && doc.Attempt.Turn != ref.TurnID) {
		return nil, scopeError(&Error{Code: "scope_mismatch", Operation: "turn.cancel", RawRecord: append(json.RawMessage(nil), raw...), Cause: errors.New("cancellation snapshot belongs to another turn")}, ref)
	}
	if len(doc.Status) == 0 || doc.Duration < 0 || int64(doc.Duration) > int64((1<<63-1)/time.Millisecond) {
		return nil, controlPayloadError("turn.cancel", ref, raw, errors.New("invalid turn snapshot status/duration"))
	}
	status := canonicalTurnStatus(enumText(doc.Status))
	out := &TurnState{Ref: ref, Status: status, Version: int64(doc.Version), QueuePosition: ptrCopy(doc.Position), RequestedProvider: doc.Provider, RequestedModel: doc.Model, RequestedMode: doc.Mode, RecoveryRequired: status == TurnRecoveryRequired, ErrorCode: doc.ErrorCode, ErrorMessage: doc.ErrorMessage, Raw: append(json.RawMessage(nil), raw...)}
	if doc.Attempt != nil {
		out.LatestAttemptID = doc.Attempt.ID
	}
	if doc.Recovery != nil {
		out.RecoveryReason = doc.Recovery.Reason
	}
	out.Usage = Usage{InputTokens: int64(doc.Usage.Input), OutputTokens: int64(doc.Usage.Output), CachedTokens: int64(doc.Usage.Cached), CostIsPartial: ptrCopy(doc.Usage.Partial), Latency: time.Duration(doc.Duration) * time.Millisecond}
	if doc.Usage.Cost != nil {
		value := int64(*doc.Usage.Cost)
		out.Usage.CostUSDTicks = &value
	}
	if doc.Usage.Incomplete != nil {
		complete := !*doc.Usage.Incomplete
		out.Usage.Complete = &complete
	}
	return out, nil
}

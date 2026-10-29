package obey

import (
	"encoding/json"
	"errors"
	"github.com/Obedience-Corp/obey-go-sdk/internal/protocol"
	"time"
)

func payloadError(message string) error {
	return &protocol.Error{Code: "protocol_error", Cause: errors.New(message)}
}
func enumText(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	return string(raw)
}
func present(raw json.RawMessage) bool { return len(raw) > 0 && string(raw) != "null" }
func payloadObject(raw json.RawMessage, out any) error {
	if !present(raw) || raw[0] != '{' {
		return payloadError("known payload object is missing")
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return &protocol.Error{Code: "protocol_error", Cause: err}
	}
	return nil
}
func positiveSequence(value protocol.Unsigned) *uint64 {
	if value == 0 {
		return nil
	}
	n := uint64(value)
	return &n
}

type wireActivity struct {
	Operator   json.RawMessage   `json:"operator_prompt"`
	Started    json.RawMessage   `json:"turn_started"`
	Finished   json.RawMessage   `json:"turn_finished"`
	Queue      json.RawMessage   `json:"queue_changed"`
	Compaction json.RawMessage   `json:"compaction"`
	Plan       json.RawMessage   `json:"plan_state"`
	Notice     json.RawMessage   `json:"exit_plan_mode_notice"`
	Admission  json.RawMessage   `json:"admission_waiting"`
	Type       json.RawMessage   `json:"type"`
	Session    string            `json:"session_id"`
	Turn       string            `json:"turn_id"`
	Sequence   protocol.Unsigned `json:"durable_sequence"`
	Thinking   json.RawMessage   `json:"thinking"`
	Completion json.RawMessage   `json:"completion"`
	Delta      json.RawMessage   `json:"message_delta"`
	ToolCall   json.RawMessage   `json:"tool_call"`
	ToolResult json.RawMessage   `json:"tool_result"`
	Approval   json.RawMessage   `json:"approval_request"`
	Error      json.RawMessage   `json:"error"`
}

func activityProjection(e *Event, raw json.RawMessage) error {
	var a wireActivity
	if err := payloadObject(raw, &a); err != nil {
		return err
	}
	if a.Session == "" || (e.Session.SessionID != "" && a.Session != e.Session.SessionID) || (e.TurnID != "" && a.Turn != e.TurnID) {
		return payloadError("activity scope differs from envelope")
	}
	e.Session.SessionID = a.Session
	e.TurnID = a.Turn
	e.DurableSequence = positiveSequence(a.Sequence)
	count := 0
	for _, arm := range []json.RawMessage{a.Thinking, a.Completion, a.Delta, a.ToolCall, a.ToolResult, a.Approval, a.Error, a.Operator, a.Started, a.Finished, a.Queue, a.Compaction, a.Plan, a.Notice, a.Admission} {
		if present(arm) {
			count++
		}
	}
	if count > 1 {
		return payloadError("activity contains multiple known oneof payloads")
	}
	knownArm := map[string]json.RawMessage{"AGENT_OPERATOR_PROMPT": a.Operator, "AGENT_TURN_STARTED": a.Started, "AGENT_TURN_FINISHED": a.Finished, "AGENT_QUEUE_CHANGED": a.Queue, "AGENT_COMPACTION": a.Compaction, "AGENT_PLAN_STATE": a.Plan, "AGENT_EXIT_PLAN_MODE_NOTICE": a.Notice, "AGENT_ADMISSION_WAITING": a.Admission}
	if body, known := knownArm[enumText(a.Type)]; known {
		var object map[string]json.RawMessage
		if err := payloadObject(body, &object); err != nil {
			return err
		}
	}
	switch enumText(a.Type) {
	case "AGENT_COMPLETION":
		var c struct {
			Content, Model string
			StopReason     string            `json:"stop_reason"`
			Input          protocol.Integer  `json:"tokens_input"`
			Output         protocol.Integer  `json:"tokens_output"`
			Cached         protocol.Integer  `json:"cached_tokens"`
			Latency        protocol.Integer  `json:"latency_ms"`
			Cost           *protocol.Integer `json:"total_cost_usd_ticks"`
			Partial        *bool             `json:"cost_is_partial"`
		}
		if err := payloadObject(a.Completion, &c); err != nil {
			return err
		}
		if c.Latency < 0 || int64(c.Latency) > int64((1<<63-1)/time.Millisecond) {
			return payloadError("completion latency is outside duration range")
		}
		usage := Usage{InputTokens: int64(c.Input), OutputTokens: int64(c.Output), CachedTokens: int64(c.Cached), Latency: time.Duration(c.Latency) * time.Millisecond, CostIsPartial: ptrCopy(c.Partial)}
		if c.Cost != nil {
			n := int64(*c.Cost)
			usage.CostUSDTicks = &n
		}
		e.Kind = EventCompletion
		e.Completion = &Completion{Text: c.Content, Model: c.Model, StopReason: c.StopReason, Usage: usage}
	case "AGENT_MESSAGE_DELTA":
		var delta struct {
			Content string `json:"content"`
		}
		if err := payloadObject(a.Delta, &delta); err != nil {
			return err
		}
		e.Kind = EventDelta
		e.Delta = &MessageDelta{Text: delta.Content, ContentKind: "assistant"}
	case "AGENT_TOOL_CALL":
		var call struct {
			ID        string `json:"call_id"`
			Name      string `json:"tool_name"`
			Arguments string `json:"arguments"`
		}
		if err := payloadObject(a.ToolCall, &call); err != nil {
			return err
		}
		e.Kind = EventToolCall
		e.ToolCall = &ToolCall{CallID: call.ID, Name: call.Name, Arguments: call.Arguments}
	case "AGENT_TOOL_RESULT":
		var result struct {
			ID      string `json:"call_id"`
			Result  string `json:"result"`
			Error   string `json:"error"`
			Success *bool  `json:"success"`
		}
		if err := payloadObject(a.ToolResult, &result); err != nil {
			return err
		}
		e.Kind = EventToolResult
		e.ToolResult = &ToolResult{CallID: result.ID, Content: result.Result, Error: result.Error, Success: ptrCopy(result.Success)}
	case "AGENT_APPROVAL_REQUEST":
		approval, err := approvalProjection(a.Approval, e.Session)
		if err != nil {
			return err
		}
		e.Kind = EventApproval
		e.Approval = &approval
	case "AGENT_ERROR":
		var failure struct {
			Code    int32  `json:"code"`
			Message string `json:"message"`
		}
		if err := payloadObject(a.Error, &failure); err != nil {
			return err
		}
		if failure.Message == "" {
			failure.Message = "daemon reported an error"
		}
		e.Kind = EventError
		e.Diagnostic = &Diagnostic{Code: "daemon_error", Message: failure.Message, Severity: "error", DaemonCode: ptrCopy(&failure.Code), Details: append(json.RawMessage(nil), raw...)}
	case "AGENT_THINKING":
		var thinking struct {
			Content string `json:"content"`
			Final   bool   `json:"is_final"`
		}
		if err := payloadObject(a.Thinking, &thinking); err != nil {
			return err
		}
		e.Kind = EventActivity // Reasoning remains distinct from assistant deltas.
	case "AGENT_OPERATOR_PROMPT", "AGENT_TURN_STARTED", "AGENT_TURN_FINISHED", "AGENT_QUEUE_CHANGED", "AGENT_EXIT_PLAN_MODE_NOTICE", "AGENT_PLAN_STATE", "AGENT_ADMISSION_WAITING", "AGENT_COMPACTION":
		e.Kind = EventActivity
	default:
		e.Kind = EventUnknown // Unknown enum/payload never becomes completion or approval.
	}
	return nil
}
func approvalProjection(raw json.RawMessage, scope SessionRef) (Approval, error) {
	var request struct {
		Call      string `json:"call_id"`
		Tool      string `json:"tool_name"`
		Arguments string `json:"arguments"`
	}
	if err := payloadObject(raw, &request); err != nil {
		return Approval{}, err
	}
	if request.Call == "" {
		return Approval{}, payloadError("approval request lacks exact call ID")
	}
	return Approval{Session: scope, CallID: request.Call, ToolName: request.Tool, Arguments: request.Arguments, Raw: append(json.RawMessage(nil), raw...)}, nil
}
func pendingApprovalProjection(raw json.RawMessage, scope SessionRef) (Approval, error) {
	var pending struct {
		Session   string          `json:"session_id"`
		Camp      string          `json:"campaign_id"`
		Request   json.RawMessage `json:"request"`
		Requested string          `json:"requested_at"`
		Expires   string          `json:"expires_at"`
	}
	if err := payloadObject(raw, &pending); err != nil {
		return Approval{}, err
	}
	if pending.Session != scope.SessionID || pending.Camp != scope.CampID {
		return Approval{}, payloadError("pending approval scope differs")
	}
	approval, err := approvalProjection(pending.Request, scope)
	if err != nil {
		return approval, err
	}
	if pending.Requested != "" {
		value, err := time.Parse(time.RFC3339Nano, pending.Requested)
		if err != nil {
			return approval, payloadError("invalid approval request timestamp")
		}
		approval.RequestedAt = &value
	}
	if pending.Expires != "" {
		value, err := time.Parse(time.RFC3339Nano, pending.Expires)
		if err != nil {
			return approval, payloadError("invalid approval expiry timestamp")
		}
		approval.ExpiresAt = &value
	}
	approval.Raw = append(json.RawMessage(nil), raw...)
	return approval, nil
}
func markerProjection(raw json.RawMessage, scope SessionRef) (*AttachMarker, error) {
	var marker struct {
		Type    json.RawMessage   `json:"type"`
		High    protocol.Unsigned `json:"high_water_sequence"`
		Turn    string            `json:"current_turn_id"`
		Status  json.RawMessage   `json:"current_turn_status"`
		Attempt string            `json:"current_attempt_id"`
		Queue   []struct {
			ID       string           `json:"turn_id"`
			Position uint32           `json:"position"`
			Status   json.RawMessage  `json:"status"`
			Version  protocol.Integer `json:"turn_version"`
			Ref      string           `json:"client_ref"`
			Preview  string           `json:"preview"`
		} `json:"queue"`
		Recovery bool              `json:"recovery_required"`
		Reason   string            `json:"recovery_reason"`
		Version  protocol.Integer  `json:"snapshot_version"`
		Requests []json.RawMessage `json:"pending_approvals"`
		Records  []json.RawMessage `json:"pending_approval_records"`
	}
	if err := payloadObject(raw, &marker); err != nil {
		return nil, err
	}
	out := &AttachMarker{Type: enumText(marker.Type), HighWaterSequence: uint64(marker.High), CurrentTurnID: marker.Turn, CurrentTurnStatus: canonicalTurnStatus(enumText(marker.Status)), CurrentAttemptID: marker.Attempt, RecoveryRequired: marker.Recovery, RecoveryReason: marker.Reason, SnapshotVersion: int64(marker.Version)}
	for _, q := range marker.Queue {
		out.Queue = append(out.Queue, QueuedTurn{TurnID: q.ID, Position: q.Position, Status: canonicalTurnStatus(enumText(q.Status)), Version: int64(q.Version), ClientRef: q.Ref, Preview: q.Preview})
	}
	if len(marker.Records) > 0 {
		for _, raw := range marker.Records {
			a, err := pendingApprovalProjection(raw, scope)
			if err != nil {
				return nil, err
			}
			out.PendingApprovals = append(out.PendingApprovals, a)
		}
	} else {
		for _, raw := range marker.Requests {
			a, err := approvalProjection(raw, scope)
			if err != nil {
				return nil, err
			}
			out.PendingApprovals = append(out.PendingApprovals, a)
		}
	}
	return out, nil
}
func projectRecord(r protocol.Envelope) (Event, error) {
	e := Event{Kind: EventUnknown, Protocol: ProtocolVersion{Name: r.Protocol, Version: r.Version}, PayloadType: r.PayloadType, Session: SessionRef{CampID: r.CampID, SessionID: r.SessionID}, TurnID: r.TurnID, RequestedSessionID: r.RequestedSessionID, ObservedSessionID: r.ObservedSessionID, TargetTurnID: r.TargetTurnID, CallID: r.CallID, DecisionIndeterminate: r.DecisionIndeterminate, OutputSource: r.OutputSource, OutputFidelity: r.OutputFidelity, HighWaterSequence: ptrCopy(r.HighWaterSequence), RawRecord: append(json.RawMessage(nil), r.Raw...), RawPayload: append(json.RawMessage(nil), r.Payload...)}
	switch r.Type {
	case "session_created":
		e.Kind = EventSessionCreated
	case "assistant_content":
		e.Kind = EventCompletion
		if r.Content != nil {
			e.Completion = &Completion{Text: *r.Content}
		}
	case "usage":
		e.Kind = EventActivity
	case "error":
		e.Kind = EventError
		severity := r.Severity
		if severity == "" {
			severity = "error"
			if r.Protocol == "obey.session.run" && r.Version == 1 && r.Code == "stop_session" {
				severity = "warning"
			}
		}
		e.Diagnostic = &Diagnostic{Code: r.Code, Message: r.Message, Severity: severity, DaemonCode: ptrCopy(r.DaemonCode), Details: append(json.RawMessage(nil), r.Details...)}
	case "terminal":
		e.Kind = EventTerminal
		e.Terminal = &Terminal{Status: r.Status, Outcome: r.Outcome, Cleanup: CleanupDisposition(r.Cleanup)}
	case "event":
		switch r.PayloadType {
		case "local.v1.SendMessageResponse":
			var wrapper struct {
				Accepted json.RawMessage `json:"accepted"`
				Event    json.RawMessage `json:"event"`
			}
			if err := payloadObject(r.Payload, &wrapper); err != nil {
				return e, err
			}
			if present(wrapper.Accepted) && present(wrapper.Event) {
				return e, payloadError("send response contains multiple known oneofs")
			}
			if present(wrapper.Accepted) {
				a, err := protocol.DecodeAcceptance(r.Payload)
				if err != nil {
					return e, err
				}
				if a.TurnID != r.TurnID || a.TurnID == "" {
					return e, payloadError("receipt scope differs from envelope")
				}
				e.Kind = EventReceipt
				position := ptrCopy(a.QueuePosition)
				if a.Status == string(TurnQueued) {
					if position == nil {
						position = new(uint32)
					}
				} else {
					position = nil
				}
				e.Receipt = &Acceptance{Ref: TurnRef{SessionRef: e.Session, TurnID: a.TurnID, IdempotencyKey: r.IdempotencyKey, ClientRef: r.ClientRef}, Status: TurnStatus(a.Status), QueuePosition: position, Deduplicated: a.Deduplicated, Raw: append(json.RawMessage(nil), a.Raw...)}
			}
			if present(wrapper.Event) {
				if err := activityProjection(&e, wrapper.Event); err != nil {
					return e, err
				}
			}
		case "local.v1.AttachSessionResponse":
			var wrapper struct {
				Backfill json.RawMessage `json:"backfill"`
				Marker   json.RawMessage `json:"marker"`
				Live     json.RawMessage `json:"live"`
			}
			if err := payloadObject(r.Payload, &wrapper); err != nil {
				return e, err
			}
			count := 0
			for _, p := range []json.RawMessage{wrapper.Backfill, wrapper.Marker, wrapper.Live} {
				if present(p) {
					count++
				}
			}
			if count > 1 {
				return e, payloadError("attach response contains multiple known oneofs")
			}
			if present(wrapper.Marker) {
				marker, err := markerProjection(wrapper.Marker, e.Session)
				if err != nil {
					return e, err
				}
				e.Kind = EventMarker
				e.Marker = marker
			}
			if present(wrapper.Backfill) {
				data, _ := json.Marshal(map[string]any{"entries": []json.RawMessage{wrapper.Backfill}})
				page, err := decodeTranscriptPage(data, e.Session)
				if err != nil {
					return e, err
				}
				if len(page.Entries) != 1 || page.Entries[0].TurnID != r.TurnID {
					return e, payloadError("backfill scope differs from envelope")
				}
				e.Kind = EventBackfill
				e.Backfill = &page.Entries[0]
				n := e.Backfill.Sequence
				if n > 0 {
					e.DurableSequence = &n
				}
			}
			if present(wrapper.Live) {
				if err := activityProjection(&e, wrapper.Live); err != nil {
					return e, err
				}
			}
		case "local.v1.WatchAllActivityResponse":
			var wrapper struct {
				Event *struct {
					Session  string            `json:"session_id"`
					Activity json.RawMessage   `json:"activity"`
					Durable  protocol.Unsigned `json:"durable_sequence"`
					Dropped  protocol.Integer  `json:"dropped_count"`
					Meta     json.RawMessage   `json:"meta_event"`
				} `json:"event"`
			}
			if err := payloadObject(r.Payload, &wrapper); err != nil {
				return e, err
			}
			if wrapper.Event == nil {
				return e, payloadError("watch response lacks event")
			}
			m := wrapper.Event
			if e.Session.SessionID != "" && m.Session != e.Session.SessionID {
				return e, payloadError("watch session differs from envelope")
			}
			e.Session.SessionID = m.Session
			e.Kind = EventActivity
			if present(m.Activity) {
				if err := activityProjection(&e, m.Activity); err != nil {
					return e, err
				}
			}
			if m.Durable > 0 {
				if e.DurableSequence != nil && *e.DurableSequence != uint64(m.Durable) {
					return e, payloadError("watch durable sequence disagrees")
				}
				e.DurableSequence = positiveSequence(m.Durable)
			}
			if m.Dropped < 0 {
				return e, payloadError("negative drop counter")
			}
			if m.Dropped > 0 {
				n := int64(m.Dropped)
				e.DroppedCount = &n
				if e.Kind == EventActivity || e.Kind == EventUnknown {
					e.Kind = EventDrop
				}
			}
		case "local.v1.ListRecentActivityResponse":
			var wrapper struct {
				Events []json.RawMessage `json:"events"`
				Page   struct {
					Next string `json:"next_cursor"`
					More bool   `json:"has_more"`
				} `json:"page"`
			}
			if err := payloadObject(r.Payload, &wrapper); err != nil {
				return e, err
			}
			page := &ActivityHistoryPage{NextCursor: wrapper.Page.Next, HasMore: wrapper.Page.More, Raw: append(json.RawMessage(nil), r.Payload...)}
			for _, raw := range wrapper.Events {
				var row struct {
					ID, Kind, Message string
					Session           string            `json:"session_id"`
					Camp              string            `json:"campaign_id"`
					Group             string            `json:"group_id"`
					Sequence          protocol.Unsigned `json:"sequence_number"`
					Type              json.RawMessage   `json:"activity_type"`
					Old               string            `json:"old_status"`
					New               string            `json:"new_status"`
					Metadata          string            `json:"metadata_json"`
					Timestamp         string            `json:"timestamp"`
				}
				if err := payloadObject(raw, &row); err != nil {
					return e, err
				}
				if e.Session.CampID != "" && row.Camp != e.Session.CampID {
					return e, payloadError("history camp differs from envelope")
				}
				page.Entries = append(page.Entries, ActivityHistoryEvent{ID: row.ID, Session: SessionRef{CampID: row.Camp, SessionID: row.Session}, GroupID: row.Group, Sequence: uint64(row.Sequence), Kind: row.Kind, Message: row.Message, ActivityType: enumText(row.Type), OldStatus: row.Old, NewStatus: row.New, Metadata: row.Metadata, Timestamp: row.Timestamp, Raw: append(json.RawMessage(nil), raw...)})
			}
			e.Kind = EventActivity
			e.History = page
		}
	}
	return e, nil
}

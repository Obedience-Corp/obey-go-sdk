package obey

import (
	"encoding/json"
	"github.com/Obedience-Corp/obey-go-sdk/internal/protocol"
	"testing"
)

func projected(t *testing.T, wrapper, payload string) Event {
	t.Helper()
	r := protocol.Envelope{Protocol: "obey.session.attach", Version: 1, Type: "event", PayloadType: wrapper, SessionID: "s", CampID: "c", TurnID: "t", Payload: json.RawMessage(payload)}
	data, _ := json.Marshal(r)
	r.Raw = data
	e, err := projectRecord(r)
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func TestPayloadCompletionDeltaToolApprovalAndRawCopies(t *testing.T) {
	e := projected(t, "local.v1.SendMessageResponse", `{"event":{"session_id":"s","turn_id":"t","durable_sequence":"9007199254740993","type":"AGENT_COMPLETION","completion":{"content":"","tokens_input":3,"tokens_output":2,"cached_tokens":"9007199254740993","latency_ms":"12","total_cost_usd_ticks":"10000000000","cost_is_partial":false}}}`)
	if e.Kind != EventCompletion || e.Completion == nil || e.Completion.Text != "" || e.Completion.Usage.CachedTokens != 9007199254740993 || e.Completion.Usage.CostUSDTicks == nil || *e.Completion.Usage.CostUSDTicks != 10000000000 || e.DurableSequence == nil || *e.DurableSequence != 9007199254740993 {
		t.Fatalf("completion=%+v", e)
	}
	copy := cloneEvent(e)
	copy.RawPayload[0] = ' '
	*copy.DurableSequence = 1
	*copy.Completion.Usage.CostUSDTicks = 2
	if e.RawPayload[0] != '{' || *e.DurableSequence == 1 || *e.Completion.Usage.CostUSDTicks == 2 {
		t.Fatal("event shares mutable values")
	}
	for _, tc := range []struct {
		payload string
		kind    EventKind
	}{{`"message_delta":{"content":"chunk"}`, EventDelta}, {`"tool_call":{"call_id":"call","tool_name":"tool","arguments":"{}"}`, EventToolCall}, {`"tool_result":{"call_id":"call","result":"value","success":false}`, EventToolResult}, {`"approval_request":{"call_id":"call","tool_name":"tool","arguments":"{}"}`, EventApproval}} {
		typ := map[EventKind]string{EventDelta: "AGENT_MESSAGE_DELTA", EventToolCall: "AGENT_TOOL_CALL", EventToolResult: "AGENT_TOOL_RESULT", EventApproval: "AGENT_APPROVAL_REQUEST"}[tc.kind]
		event := projected(t, "local.v1.SendMessageResponse", `{"event":{"session_id":"s","turn_id":"t","type":"`+typ+`",`+tc.payload+`}}`)
		if event.Kind != tc.kind {
			t.Fatalf("event=%+v", event)
		}
	}
	future := projected(t, "local.v1.SendMessageResponse", `{"event":{"session_id":"s","turn_id":"t","type":999,"completion":{"content":"not evidence"}}}`)
	if future.Kind != EventUnknown || future.Completion != nil || len(future.RawPayload) == 0 {
		t.Fatalf("future promoted=%+v", future)
	}
}
func TestPayloadMarkerBackfillHistoryAndDropRemainDistinct(t *testing.T) {
	marker := projected(t, "local.v1.AttachSessionResponse", `{"marker":{"type":"ATTACH_MARKER_TYPE_BACKFILL_COMPLETE","high_water_sequence":"0","snapshot_version":"9007199254740993","queue":[{"turn_id":"queued","position":0,"status":"TURN_QUEUED","turn_version":"9007199254740993"}],"pending_approval_records":[{"session_id":"s","campaign_id":"c","request":{"call_id":"call","tool_name":"tool"},"requested_at":"2026-10-27T20:00:00Z","expires_at":"2026-10-27T20:01:00Z"}]}}`)
	if marker.Kind != EventMarker || marker.Marker.HighWaterSequence != 0 || marker.Marker.SnapshotVersion != 9007199254740993 || marker.Marker.Queue[0].Position != 0 || marker.Marker.PendingApprovals[0].ExpiresAt == nil {
		t.Fatalf("marker=%+v", marker)
	}
	copied := cloneEvent(marker)
	copied.Marker.Queue[0].Position = 2
	copied.Marker.PendingApprovals[0].Raw[0] = ' '
	if marker.Marker.Queue[0].Position != 0 || marker.Marker.PendingApprovals[0].Raw[0] != '{' {
		t.Fatal("snapshot aliases")
	}
	backfill := projected(t, "local.v1.AttachSessionResponse", `{"backfill":{"id":"entry","session_id":"s","campaign_id":"c","sequence_number":"12","turn_id":"t","attempt_id":"a","kind":"completion","activity_type":"AGENT_COMPLETION","content":"old"}}`)
	if backfill.Kind != EventBackfill || backfill.Completion != nil || backfill.Backfill.Sequence != 12 {
		t.Fatalf("backfill promoted=%+v", backfill)
	}
	drop := projected(t, "local.v1.WatchAllActivityResponse", `{"event":{"session_id":"s","sequence_number":"999","durable_sequence":"0","dropped_count":"9007199254740993","meta_event":"META_EVENT_KEEPALIVE"}}`)
	if drop.Kind != EventDrop || drop.DurableSequence != nil || drop.DroppedCount == nil || *drop.DroppedCount != 9007199254740993 {
		t.Fatalf("drop=%+v", drop)
	}
	history := projected(t, "local.v1.ListRecentActivityResponse", `{"events":[{"id":"history","session_id":"other","campaign_id":"c","sequence_number":"9007199254740993","activity_type":999,"kind":"future","message":"raw"}],"page":{"has_more":true,"next_cursor":"opaque"}}`)
	if history.History == nil || !history.History.HasMore || history.History.NextCursor != "opaque" || history.History.Entries[0].Sequence != 9007199254740993 || history.History.Entries[0].ActivityType != "999" {
		t.Fatalf("history=%+v", history)
	}
}
func TestPayloadKnownMalformedFailsUnknownArmSurvives(t *testing.T) {
	for _, payload := range []string{`{"event":{"session_id":"s","turn_id":"t","type":"AGENT_COMPLETION"}}`, `{"event":{"session_id":"s","turn_id":"t","type":"AGENT_ERROR"}}`, `{"event":{"session_id":"foreign","turn_id":"t","type":"AGENT_COMPLETION","completion":{}}}`, `{"event":{"session_id":"s","turn_id":"t","type":"AGENT_COMPLETION","completion":{},"message_delta":{}}}`} {
		_, err := projectRecord(protocol.Envelope{Type: "event", PayloadType: "local.v1.SendMessageResponse", SessionID: "s", CampID: "c", TurnID: "t", Payload: json.RawMessage(payload)})
		if err == nil {
			t.Fatalf("malformed accepted %s", payload)
		}
	}
	unknown := projected(t, "local.v1.AttachSessionResponse", `{"future":{"completion":{"content":"not inference"}}}`)
	if unknown.Kind != EventUnknown || unknown.Completion != nil {
		t.Fatal("unknown arm promoted")
	}
	emptyError := projected(t, "local.v1.SendMessageResponse", `{"event":{"session_id":"s","turn_id":"t","type":"AGENT_ERROR","error":{"code":42}}}`)
	if emptyError.Diagnostic == nil || emptyError.Diagnostic.Message == "" || emptyError.Diagnostic.DaemonCode == nil || *emptyError.Diagnostic.DaemonCode != 42 {
		t.Fatalf("empty error=%+v", emptyError)
	}
}

func TestPayloadLifecycleArmPresenceAndOneof(t *testing.T) {
	for _, typ := range []string{"AGENT_TURN_STARTED", "AGENT_TURN_FINISHED", "AGENT_QUEUE_CHANGED", "AGENT_PLAN_STATE", "AGENT_EXIT_PLAN_MODE_NOTICE", "AGENT_ADMISSION_WAITING", "AGENT_COMPACTION", "AGENT_OPERATOR_PROMPT"} {
		raw := json.RawMessage(`{"event":{"session_id":"s","turn_id":"t","type":"` + typ + `"}}`)
		if _, err := projectRecord(protocol.Envelope{Type: "event", PayloadType: "local.v1.SendMessageResponse", SessionID: "s", CampID: "c", TurnID: "t", Payload: raw}); err == nil {
			t.Fatalf("missing known arm accepted%s", typ)
		}
	}
	raw := json.RawMessage(`{"event":{"session_id":"s","turn_id":"t","type":"AGENT_COMPLETION","completion":{},"turn_finished":{"status":"TURN_COMPLETED"}}}`)
	if _, err := projectRecord(protocol.Envelope{Type: "event", PayloadType: "local.v1.SendMessageResponse", SessionID: "s", CampID: "c", TurnID: "t", Payload: raw}); err == nil {
		t.Fatal("multiple known arms accepted")
	}
	receipt := projected(t, "local.v1.SendMessageResponse", `{"accepted":{"turn_id":"t","status":"TURN_COMPLETED","queue_position":3}}`)
	if receipt.Receipt == nil || receipt.Receipt.QueuePosition != nil {
		t.Fatalf("nonqueued position=%+v", receipt)
	}
	failed := projected(t, "local.v1.SendMessageResponse", `{"event":{"session_id":"s","turn_id":"t","type":"AGENT_TURN_FINISHED","turn_finished":{"status":"TURN_FAILED"}}}`)
	if failed.Kind != EventActivity {
		t.Fatalf("observation failed turn not surfaced=%+v", failed)
	}
}

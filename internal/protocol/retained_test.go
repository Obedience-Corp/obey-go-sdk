package protocol

import (
	"encoding/json"
	"testing"
)

func retainedFixture(t *testing.T) fixture {
	t.Helper()
	data, err := corpus.ReadFile("testdata/fixtures/send_wait_persisted_result.json")
	if err != nil {
		t.Fatal(err)
	}
	var f fixture
	if json.Unmarshal(data, &f) != nil {
		t.Fatal("fixture")
	}
	return f
}
func TestRetainedProofRequiresMatchingCutAttemptAndScope(t *testing.T) {
	for _, mode := range []string{"valid-empty", "wrong-attempt", "missing-boundary", "terminal-cut", "foreign-state", "unknown-state"} {
		t.Run(mode, func(t *testing.T) {
			f := retainedFixture(t)
			var state, page, terminal map[string]any
			_ = json.Unmarshal(f.Records[1], &state)
			_ = json.Unmarshal(f.Records[2], &page)
			_ = json.Unmarshal(f.Records[3], &terminal)
			turn := state["payload"].(map[string]any)["turn"].(map[string]any)
			entry := page["payload"].(map[string]any)["entries"].([]any)[0].(map[string]any)
			switch mode {
			case "wrong-attempt":
				entry["attempt_id"] = "old"
			case "missing-boundary":
				entry["sequence_number"] = "9007199254740992"
			case "terminal-cut":
				terminal["high_water_sequence"] = "9007199254740994"
			case "foreign-state":
				turn["session_id"] = "foreign"
			case "unknown-state":
				turn["status"] = 999
			}
			f.Records[1], _ = json.Marshal(state)
			f.Records[2], _ = json.Marshal(page)
			f.Records[3], _ = json.Marshal(terminal)
			d := Decoder{Protocol: f.Operation, Version: 1}
			var err error
			for _, record := range f.Records {
				_, err = d.Add(record)
				if err != nil {
					break
				}
			}
			if err == nil {
				err = d.Finish(0)
			}
			if (err == nil) != (mode == "valid-empty") {
				t.Fatalf("mode%s error=%v", mode, err)
			}
		})
	}
}
func TestForwardedAttachPreservesOriginalTargetWithoutPromotingSibling(t *testing.T) {
	f := retainedFixture(t)
	d := Decoder{Protocol: f.Operation, Version: 1}
	if _, err := d.Add(f.Records[0]); err != nil {
		t.Fatal(err)
	}
	record := Envelope{Protocol: f.Operation, Version: 1, Type: "event", PayloadType: "local.v1.AttachSessionResponse", CampID: "c1", SessionID: "successor", RequestedSessionID: "s1", ObservedSessionID: "successor", TargetTurnID: "t1", TurnID: "sibling", Payload: json.RawMessage(`{"live":{"session_id":"successor","turn_id":"sibling","type":"AGENT_COMPLETION","completion":{"content":"wrong"}}}`)}
	raw, _ := json.Marshal(record)
	if _, err := d.Add(raw); err != nil {
		t.Fatal(err)
	}
	if d.SessionID != "s1" || d.TurnID != "t1" || d.Completion {
		t.Fatalf("promoted observed scope=%+v", d)
	}
	record.TargetTurnID = "foreign"
	raw, _ = json.Marshal(record)
	if _, err := d.Add(raw); err == nil {
		t.Fatal("foreign target accepted")
	}
	if d.TurnID != "t1" {
		t.Fatal("failed frame mutated target")
	}
}

package protocol

import (
	"encoding/json"
	"testing"
)

func TestRunV2RequiresOriginalSingleCompletedAttempt(t *testing.T) {
	data, _ := corpus.ReadFile("testdata/fixtures/v2_completed.json")
	var fixture fixture
	if json.Unmarshal(data, &fixture) != nil {
		t.Fatal("fixture")
	}
	for _, mode := range []string{"valid", "missing-state", "wrong-session", "second-attempt", "missing-attempt-id", "running", "late-error", "failed-finished", "approval"} {
		t.Run(mode, func(t *testing.T) {
			d := Decoder{Protocol: fixture.Operation, Version: 2}
			var err error
			for _, original := range fixture.Records {
				var r Envelope
				_ = json.Unmarshal(original, &r)
				if r.PayloadType == "local.v1.GetTurnResponse" {
					if mode == "missing-state" {
						continue
					}
					var p map[string]any
					_ = json.Unmarshal(r.Payload, &p)
					turn := p["turn"].(map[string]any)
					attempt := turn["latest_attempt"].(map[string]any)
					switch mode {
					case "wrong-session":
						turn["session_id"] = "foreign"
					case "second-attempt":
						attempt["attempt_number"] = "2"
					case "missing-attempt-id":
						attempt["id"] = ""
					case "running":
						turn["status"] = "TURN_RUNNING"
					}
					r.Payload, _ = json.Marshal(p)
				}
				if (mode == "failed-finished" || mode == "approval") && r.Type == "terminal" {
					payload := `{"event":{"type":"AGENT_TURN_FINISHED","session_id":"s1","turn_id":"t1","turn_finished":{"status":"TURN_FAILED"}}}`
					if mode == "approval" {
						payload = `{"event":{"type":"AGENT_APPROVAL_REQUEST","session_id":"s1","turn_id":"t1","approval_request":{"call_id":"call"}}}`
					}
					raw, _ := json.Marshal(Envelope{Protocol: fixture.Operation, Version: 2, Type: "event", SessionID: "s1", CampID: "c1", TurnID: "t1", PayloadType: "local.v1.SendMessageResponse", Payload: json.RawMessage(payload)})
					_, err = d.Add(raw)
					if err != nil {
						break
					}
				}
				if mode == "late-error" && r.Type == "terminal" {
					bad := Envelope{Protocol: fixture.Operation, Version: 2, Type: "event", SessionID: "s1", CampID: "c1", TurnID: "t1", PayloadType: "local.v1.SendMessageResponse", Payload: json.RawMessage(`{"event":{"type":"AGENT_ERROR","session_id":"s1","turn_id":"t1","error":{"code":42,"message":"late"}}}`)}
					raw, _ := json.Marshal(bad)
					_, err = d.Add(raw)
					if err != nil {
						break
					}
				}
				raw, _ := json.Marshal(r)
				_, err = d.Add(raw)
				if err != nil {
					break
				}
			}
			if err == nil {
				err = d.Finish(0)
			}
			if (err == nil) != (mode == "valid") {
				t.Fatalf("mode%s err=%v", mode, err)
			}
		})
	}
}

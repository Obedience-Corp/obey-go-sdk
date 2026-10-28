package obey

import (
	"encoding/json"
	"github.com/obediencecorp/obey-go-sdk/internal/protocol"
	"testing"
)

func TestTranscriptExactScopeRawAndIntegers(t *testing.T) {
	data := []byte(`{"entries":[{"id":"e","session_id":"s","campaign_id":"c","sequence_number":"18446744073709551615","activity_type":"AGENT_COMPLETION","kind":"completion","content":"","turn_id":"t","attempt_id":"a","success":false,"metadata_json":"{\"future\":9007199254740993}","unknown":true}],"page":{"has_more":true,"next_cursor":"opaque+cursor"}}`)
	page, err := decodeTranscriptPage(data, SessionRef{"c", "s"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 1 || page.Entries[0].Sequence != ^uint64(0) || page.Entries[0].Success == nil || *page.Entries[0].Success || page.Entries[0].ActivityType != "AGENT_COMPLETION" || !page.HasMore || page.NextCursor != "opaque+cursor" || !json.Valid(page.Entries[0].Metadata) {
		t.Fatalf("page=%+v", page)
	}
	var raw map[string]any
	if json.Unmarshal(page.Entries[0].Raw, &raw) != nil || raw["unknown"] != true {
		t.Fatal("unknown raw lost")
	}
	data[0] = ' '
	if page.Raw[0] != '{' {
		t.Fatal("page raw aliases input")
	}
	for _, scope := range []SessionRef{{"wrong", "s"}, {"c", "wrong"}} {
		if _, err := decodeTranscriptPage(page.Raw, scope); err == nil {
			t.Fatal("foreign transcript accepted")
		}
	}
	for _, number := range []string{"1.1", "1e2", "-1", "18446744073709551616"} {
		var n protocol.Unsigned
		if json.Unmarshal([]byte(number), &n) == nil {
			t.Fatalf("invalid exact sequence %s", number)
		}
	}
}
func TestWaitCompletedAttemptAllowsRetries(t *testing.T) {
	turn := &Turn{ref: TurnRef{SessionRef: SessionRef{"c", "s"}, TurnID: "t"}}
	for _, tc := range []struct {
		attempt string
		valid   bool
	}{{`{"id":"a","turn_id":"t","attempt_number":2,"state":"completed"}`, true}, {`{"id":"a","turn_id":"t","attempt_number":0,"state":"completed"}`, false}, {`{"id":"a","turn_id":"other","attempt_number":2,"state":"completed"}`, false}, {`{"id":"a","turn_id":"t","attempt_number":2,"state":"failed"}`, false}, {`null`, false}} {
		state := &TurnState{Raw: json.RawMessage(`{"schema_version":"obey.turn.v1","turn":{"id":"t","session_id":"s","campaign_id":"c","status":"completed","latest_attempt":` + tc.attempt + `}}`)}
		_, err := turn.completedAttempt(state)
		if (err == nil) != tc.valid {
			t.Fatalf("attempt%s err=%v", tc.attempt, err)
		}
	}
}

func TestScopeErrorNilRemainsSuccess(t *testing.T) {
	if scopeError(nil, TurnRef{}) != nil {
		t.Fatal("nil converted into error")
	}
}

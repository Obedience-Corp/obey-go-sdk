package protocol

import (
	"encoding/json"
	"testing"
)

func TestDecoderFailedFrameDoesNotCommitIdentity(t *testing.T) {
	good := []byte(`{"protocol":"obey.session.create","version":1,"type":"session_created","session_id":"known","campaign_id":"camp","payload_type":"local.v1.CreateSessionResponse","payload":{"session":{"id":"known","campaign_id":"camp"}}}`)
	for _, bad := range []string{
		`{"protocol":"obey.session.create","version":1,"type":"error","session_id":"foreign","campaign_id":"camp","code":"indeterminate_creation","message":"unknown","severity":"error"}`,
		`{"protocol":"obey.session.create","version":1,"type":"error","session_id":"known","campaign_id":"foreign","code":"indeterminate_creation","message":"unknown","severity":"error"}`,
		`{"protocol":"obey.session.create","version":1,"type":"session_created","session_id":"foreign","campaign_id":"camp","payload_type":"local.v1.CreateSessionResponse","payload":{"session":{"id":"other","campaign_id":"camp"}}}`,
	} {
		decoder := Decoder{Protocol: "obey.session.create", Version: 1}
		if _, err := decoder.Add(good); err != nil {
			t.Fatal(err)
		}
		if _, err := decoder.Add([]byte(bad)); err == nil {
			t.Fatal("invalid frame accepted")
		}
		if decoder.SessionID != "known" || decoder.CampID != "camp" || !decoder.Created || decoder.Terminal {
			t.Fatalf("failed frame committed state: %+v", decoder)
		}
		if _, err := decoder.Add([]byte(`{"protocol":"obey.session.create","version":1,"type":"terminal","session_id":"known","campaign_id":"camp","status":"completed","outcome":"session_created","cleanup":"not_owned"}`)); err != nil {
			t.Fatal(err)
		}
		if err := decoder.Finish(0); err != nil {
			t.Fatal(err)
		}
	}
	decoder := Decoder{Protocol: "obey.session.create", Version: 1}
	malformed := []byte(`{"protocol":"obey.session.create","version":1,"type":"session_created","session_id":"untrusted","campaign_id":"camp","payload_type":"local.v1.CreateSessionResponse","payload":{"session":{"id":"other","campaign_id":"camp"}}}`)
	if _, err := decoder.Add(malformed); err == nil || decoder.SessionID != "" || decoder.CampID != "" || decoder.Created {
		t.Fatalf("malformed first record established identity: %+v %v", decoder, err)
	}
}
func TestDecoderDiagnosticLimitRollback(t *testing.T) {
	raw := []byte(`{"protocol":"obey.session.create","version":1,"type":"error","campaign_id":"camp","code":"notice","message":"warning","severity":"warning"}`)
	decoder := Decoder{Protocol: "obey.session.create", Version: 1, MaxDiagnosticBytes: int64(len(raw))}
	if _, err := decoder.Add(raw); err != nil {
		t.Fatal(err)
	}
	first, _ := json.Marshal(decoder.Errors)
	if _, err := decoder.Add(raw); err == nil {
		t.Fatal("diagnostic budget ignored")
	}
	after, _ := json.Marshal(decoder.Errors)
	if string(first) != string(after) || len(decoder.Errors) != 1 || decoder.diagnosticBytes != int64(len(raw)) {
		t.Fatal("failed diagnostic append changed accepted history")
	}
}

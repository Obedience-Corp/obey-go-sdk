package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/obediencecorp/obey-go-sdk/pkg/obey"
)

func TestMalformedDiagnosticsPreserveKnownJournalIdentity(t *testing.T) {
	raw := []byte{'{', 'b', 'a', 'd', 0, 255}
	detail := []byte("not-json\x00")
	ref := obey.TurnRef{SessionRef: obey.SessionRef{CampID: "camp", SessionID: "session"}, TurnID: "accepted-turn", IdempotencyKey: "original-key"}
	sdkErr := &obey.Error{Code: "protocol_error", Session: ref.SessionRef, TurnID: ref.TurnID, IdempotencyKey: ref.IdempotencyKey, AcceptanceIndeterminate: true, RawRecord: raw, Details: detail, Diagnostics: []obey.Diagnostic{{Code: "protocol_error", Message: "original malformed stream", Severity: "error", Details: raw}}}
	record := &job{ID: "job", Key: ref.IdempotencyKey, Session: ref.SessionRef, Ref: &ref, Phase: "uncertain", LastError: describe(sdkErr)}
	data, err := json.Marshal(reply{Job: record, Error: describe(sdkErr), Result: resultForHTTP(&obey.Result{Session: ref.SessionRef, TurnID: ref.TurnID, Diagnostics: sdkErr.Diagnostics})})
	if err != nil || !json.Valid(data) {
		t.Fatalf("journal/HTTP serialization failed: %v", err)
	}
	var decoded reply
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Job == nil || decoded.Job.Ref == nil || *decoded.Job.Ref != ref || decoded.Job.LastError == nil || !decoded.Job.LastError.AcceptanceIndeterminate || decoded.Job.LastError.RawEncoding != "base64" || !bytes.Equal(decoded.Job.LastError.Raw, raw) || !bytes.Equal(decoded.Job.LastError.Details, detail) || decoded.Job.LastError.DetailsEncoding != "base64" {
		t.Fatalf("known identity/raw journal changed: %s", data)
	}
	if decoded.Error == nil || !bytes.Equal(decoded.Error.Raw, raw) || len(decoded.Error.Diagnostics) != 1 || !bytes.Equal(decoded.Error.Diagnostics[0].Details, raw) || decoded.Error.Diagnostics[0].DetailsEncoding != "base64" || decoded.Result == nil || len(decoded.Result.Diagnostics) != 1 || !bytes.Equal(decoded.Result.Diagnostics[0].Details, raw) {
		t.Fatalf("HTTP raw diagnostic changed: %s", data)
	}
	raw[0] = '!'
	detail[0] = '!'
	if bytes.Equal(record.LastError.Raw, raw) || bytes.Equal(record.LastError.Details, detail) {
		t.Fatal("diagnostic bytes alias SDK error")
	}
}

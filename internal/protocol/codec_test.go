package protocol

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

//go:embed testdata/fixtures/*.json
var corpus embed.FS

type fixture struct {
	Name, Operation, Schema, Expected string
	Exit                              int `json:"exit_code"`
	Records                           []json.RawMessage
	Raw                               string `json:"raw_stdout"`
}

func TestCorpus(t *testing.T) {
	entries, err := corpus.ReadDir("testdata/fixtures")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		t.Run(entry.Name(), func(t *testing.T) {
			data, _ := corpus.ReadFile("testdata/fixtures/" + entry.Name())
			var f fixture
			if err := json.Unmarshal(data, &f); err != nil {
				t.Fatal(err)
			}
			switch f.Schema {
			case "doctor":
				d, err := DecodeDoctor(f.Records[0])
				if err != nil {
					t.Fatal(err)
				}
				key, name := "session_run", "obey.session.run"
				if f.Name == "doctor_reads_supported" {
					key, name = "machine_read_errors", "obey.machine.error"
				}
				if f.Name == "old_binary_persistent_read" && d.Supports("machine_read_errors", "obey.machine.error", 1) {
					t.Fatal("old binary falsely supports read errors")
				}
				if !d.Supports(key, name, 1) {
					t.Fatal("v1 capability missing")
				}
			case "turn":
				d, err := DecodeTurn(f.Records[0])
				if err != nil || d.Turn.Ordinal != 9007199254740993 || d.Turn.ID != "t1" {
					t.Fatalf("%+v %v", d, err)
				}
			case "read-error":
				r, err := DecodeReadError(f.Records[0], f.Operation)
				if err != nil || r.Code != f.Expected {
					t.Fatalf("%+v %v", r, err)
				}
			case "record", "run-v1":
				if f.Name == "record_limit" {
					return
				} // lazy generated bound covered in runner tests
				if f.Name == "malformed_json" {
					if _, err := Decode([]byte(f.Raw), f.Operation, 2); err == nil {
						t.Fatal("malformed accepted")
					}
					return
				}
				version := 1
				if f.Operation == "obey.session.run" && f.Schema != "run-v1" {
					version = 2
				}
				d := Decoder{Protocol: f.Operation, Version: version}
				var got error
				for _, r := range f.Records {
					if _, got = d.Add(r); got != nil {
						break
					}
				}
				if got == nil {
					got = d.Finish(f.Exit)
				}
				want := f.Expected
				switch want {
				case "success", "success_with_cleanup_warning", "accepted":
					if got != nil {
						t.Fatal(got)
					}
				case "detached", "detached_with_drop":
					var e *Error
					if !errors.As(got, &e) || e.Code != "canceled" {
						t.Fatalf("%v", got)
					}
				default:
					var e *Error
					if !errors.As(got, &e) || e.Code != want {
						t.Fatalf("want%s got%v", want, got)
					}
				}
			default:
				// Legacy raw and durable-read projections are preserved verbatim. Their
				// state/wait/provider interpretation is exercised by later owning tasks.
				for _, record := range f.Records {
					if !json.Valid(record) {
						t.Fatalf("invalid retained raw %s", record)
					}
				}
			}
		})
	}
}
func TestDoctorRejectsMalformedProjection(t *testing.T) {
	for _, s := range []string{`{}`, `{"schema":"future","version":3,"capabilities":{}}`, `{"schema":"obey.doctor","version":3,"capabilities":{"x":{"protocol":"p","versions":[1.5]}}}`, `{"schema":"obey.doctor","version":3,"capabilities":{"x":{"protocol":"p","versions":["1"]}}}`} {
		if _, err := DecodeDoctor([]byte(s)); err == nil {
			t.Fatal(s)
		}
	}
}
func TestScopeAndSuccessEvidence(t *testing.T) {
	for _, s := range []string{`{"protocol":"obey.session.run","version":1,"type":"terminal","status":"completed"}`, `{"protocol":"obey.session.run","version":1,"type":"usage"}`} {
		d := Decoder{Protocol: "obey.session.run", Version: 1}
		_, err := d.Add([]byte(s))
		if err == nil {
			err = d.Finish(0)
		}
		if err == nil {
			t.Fatal(s)
		}
	}
	r, err := Decode([]byte(`{"protocol":"obey.session.run","version":1,"type":"usage","cached_tokens":9007199254740993}`), "obey.session.run", 1)
	if err != nil || r.CachedTokens == nil || *r.CachedTokens != 9007199254740993 {
		t.Fatalf("%+v %v", r, err)
	}
}
func TestRawCopies(t *testing.T) {
	data := []byte(`{"protocol":"obey.session.run","version":2,"type":"future","opaque":991}`)
	r, err := Decode(data, "obey.session.run", 2)
	if err != nil {
		t.Fatal(err)
	}
	data[0] = 'x'
	if !json.Valid(r.Raw) {
		t.Fatal("raw aliases caller")
	}
	var retained map[string]json.RawMessage
	if err := json.Unmarshal(r.Raw, &retained); err != nil || string(retained["opaque"]) != "991" {
		t.Fatal(fmt.Sprint(retained), err)
	}
}

func TestUnknownWrapperCannotCompleteRun(t *testing.T) {
	d := Decoder{Protocol: "obey.session.run", Version: 2}
	records := []string{
		`{"protocol":"obey.session.run","version":2,"type":"session_created","session_id":"s","campaign_id":"c","payload_type":"local.v1.CreateSessionResponse","payload":{"session":{"id":"s","campaign_id":"c"}}}`,
		`{"protocol":"obey.session.run","version":2,"type":"event","session_id":"s","campaign_id":"c","turn_id":"t","payload_type":"future.Wrapper","payload":{"event":{"type":"AGENT_COMPLETION","session_id":"s","turn_id":"t","completion":{"content":"fake"}}}}`,
		`{"protocol":"obey.session.run","version":2,"type":"terminal","status":"completed","outcome":"turn_completed","cleanup":"stopped","session_id":"s","campaign_id":"c","turn_id":"t"}`,
	}
	for _, record := range records {
		if _, err := d.Add([]byte(record)); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.Finish(0); err == nil {
		t.Fatal("unknown wrapper counted as completion")
	}
}
func TestObservationPreservesMultipleScopes(t *testing.T) {
	for _, name := range []string{"obey.session.attach", "obey.observe"} {
		t.Run(name, func(t *testing.T) {
			d := Decoder{Protocol: name, Version: 1}
			for i := 0; i < 2; i++ {
				payload := "local.v1.AttachSessionResponse"
				if name == "obey.observe" {
					payload = "local.v1.WatchAllActivityResponse"
				}
				data := fmt.Sprintf(`{"protocol":%q,"version":1,"type":"event","session_id":"s%d","campaign_id":"c","turn_id":"t%d","payload_type":%q,"payload":{"live":{"session_id":"s%d","turn_id":"t%d"}}}`, name, i, i, payload, i, i)
				r, err := d.Add([]byte(data))
				if err != nil || r.SessionID != fmt.Sprintf("s%d", i) || r.TurnID != fmt.Sprintf("t%d", i) {
					t.Fatalf("%+v %v", r, err)
				}
			}
			data := fmt.Sprintf(`{"protocol":%q,"version":1,"type":"terminal","status":"canceled","outcome":"detached","cleanup":"not_owned"}`, name)
			if _, err := d.Add([]byte(data)); err != nil {
				t.Fatal(err)
			}
			var pe *Error
			if err := d.Finish(1); !errors.As(err, &pe) || pe.Code != "canceled" {
				t.Fatal(err)
			}
		})
	}
}

func TestDiagnosticRetentionBound(t *testing.T) {
	d := Decoder{Protocol: "obey.session.run", Version: 2, MaxDiagnosticBytes: 200}
	record := []byte(`{"protocol":"obey.session.run","version":2,"type":"error","severity":"warning","code":"future","message":"detail"}`)
	if _, err := d.Add(record); err != nil {
		t.Fatal(err)
	}
	var pe *Error
	if _, err := d.Add(record); !errors.As(err, &pe) || pe.Code != "output_limit" {
		t.Fatal(err)
	}
}

package obey

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPrepareRunValidationAndCopy(t *testing.T) {
	c := &Client{config: clientConfig{camp: "default-camp", defaults: SessionOptions{Provider: "p", Model: "m", Config: json.RawMessage(`{"x":1}`)}}}
	negative := -time.Second
	for _, opts := range []*RunOptions{{Timeout: &negative}, {SessionOptions: SessionOptions{Config: json.RawMessage(`[]`)}}, {SessionOptions: SessionOptions{Config: json.RawMessage(`null`)}}} {
		if _, err := c.prepareRun(opts); err == nil {
			t.Fatal("invalid options accepted")
		}
	}
	opts := &RunOptions{SessionOptions: SessionOptions{Camp: "explicit"}, Files: []string{"original"}}
	got, err := c.prepareRun(opts)
	if err != nil {
		t.Fatal(err)
	}
	opts.Files[0] = "changed"
	opts.Camp = "changed"
	if got.Camp != "explicit" || got.Provider != "p" || got.Files[0] != "original" || got.Timeout != nil {
		t.Fatal(got)
	}
}

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Recorder-only coverage creates no listeners, processes or filesystem fixtures.
func TestCompletionFixtureValidatesAndStreams(t *testing.T) {
	f := &completionFixture{}
	for _, tc := range []struct {
		body, auth string
		status     int
	}{
		{`{"model":"fixture-model","stream":true,"messages":[{"role":"user","content":"test"}]}`, "", http.StatusUnauthorized},
		{`{"model":"foreign","stream":true,"messages":[{}]}`, "Bearer " + fixtureKey, http.StatusBadRequest},
	} {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(tc.body))
		req.Header.Set("Authorization", tc.auth)
		out := httptest.NewRecorder()
		f.ServeHTTP(out, req)
		if out.Code != tc.status || f.count() != 0 {
			t.Fatalf("invalid request status=%d count=%d", out.Code, f.count())
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"fixture-model","stream":true,"messages":[{"role":"user","content":"test"}]}`))
	req.Header.Set("Authorization", "Bearer "+fixtureKey)
	out := httptest.NewRecorder()
	f.ServeHTTP(out, req)
	if out.Code != http.StatusOK || out.Header().Get("Content-Type") != "text/event-stream" || f.count() != 1 {
		t.Fatalf("stream status=%d count=%d", out.Code, f.count())
	}
	var text string
	var done bool
	for _, line := range strings.Split(out.Body.String(), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			done = true
			continue
		}
		var frame struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &frame); err != nil {
			t.Fatal(err)
		}
		for _, choice := range frame.Choices {
			text += choice.Delta.Content
		}
	}
	if text != answer || !done {
		t.Fatalf("text=%q done=%v", text, done)
	}
}

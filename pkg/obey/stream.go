package obey

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"sync/atomic"

	"github.com/obediencecorp/obey-go-sdk/internal/execclient"
	"github.com/obediencecorp/obey-go-sdk/internal/protocol"
)

type RunStream struct {
	process           *execclient.Process
	ctx               context.Context
	builder           *resultBuilder
	fidelity          StreamFidelity
	active            atomic.Bool
	mu                sync.Mutex
	event             Event
	err, misuse       error
	exhausted, cached bool
	result            *Result
	parseErr          error
}

func (s *RunStream) Fidelity() StreamFidelity { return s.fidelity }
func (s *RunStream) misuseError() error {
	return &Error{Code: "stream_misuse", Operation: "run", Cause: errors.New("Next and Result require one consumer")}
}
func (s *RunStream) Next() bool {
	if !s.active.CompareAndSwap(false, true) {
		s.mu.Lock()
		s.misuse = s.misuseError()
		s.mu.Unlock()
		return false
	}
	defer s.active.Store(false)
	if s.exhausted {
		return false
	}
	record, err := s.process.Next(s.ctx)
	if err != nil {
		s.exhausted = true
		if err == io.EOF || s.process.Finished() {
			s.drain()
			s.complete(s.process.Wait())
		} else {
			s.mu.Lock()
			s.err = s.publicError(err)
			s.mu.Unlock()
		}
		return false
	}
	r, err := s.builder.add(record)
	if err != nil {
		s.parseErr = err
		s.exhausted = true
		s.process.Cancel(err)
		s.mu.Lock()
		s.err = s.publicError(err)
		s.mu.Unlock()
		return false
	}
	event := s.project(r)
	s.mu.Lock()
	s.event = event
	s.mu.Unlock()
	return true
}
func (s *RunStream) Event() Event { s.mu.Lock(); defer s.mu.Unlock(); return cloneEvent(s.event) }
func (s *RunStream) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.misuse != nil {
		return s.misuse
	}
	return cloneError(s.err)
}
func (s *RunStream) Result() (*Result, error) {
	if !s.active.CompareAndSwap(false, true) {
		return nil, s.misuseError()
	}
	defer s.active.Store(false)
	s.mu.Lock()
	if s.cached {
		r, e := cloneResult(s.result), cloneError(s.err)
		s.mu.Unlock()
		return r, e
	}
	s.mu.Unlock()
	s.drain()
	s.complete(s.process.Wait())
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneResult(s.result), cloneError(s.err)
}
func (s *RunStream) complete(out execclient.Outcome) {
	result, err := s.builder.finish(out, s.parseErr)
	s.mu.Lock()
	s.result = cloneResult(result)
	s.err = err
	s.cached = true
	s.exhausted = true
	s.mu.Unlock()
}
func (s *RunStream) Close() error { return s.process.Close() }
func (s *RunStream) project(r protocol.Envelope) Event {
	e := Event{Kind: EventUnknown, Protocol: s.builder.result.Protocol, PayloadType: r.PayloadType, Session: s.builder.result.Session, TurnID: s.builder.result.TurnID, RequestedSessionID: r.RequestedSessionID, RawRecord: append(json.RawMessage(nil), r.Raw...), RawPayload: append(json.RawMessage(nil), r.Payload...)}
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
		e.Diagnostic = &Diagnostic{Code: r.Code, Message: r.Message, Severity: r.Severity, DaemonCode: ptrCopy(r.DaemonCode), Details: append(json.RawMessage(nil), r.Details...)}
	case "terminal":
		e.Kind = EventTerminal
		e.Terminal = &Terminal{Status: r.Status, Outcome: r.Outcome, Cleanup: s.builder.result.Cleanup}
	}
	return e
}

func (s *RunStream) publicError(err error) *Error {
	code := "protocol_error"
	var pe *protocol.Error
	if errors.As(err, &pe) {
		code = pe.Code
	} else if errors.Is(err, execclient.ErrOutputLimit) {
		code = "output_limit"
	} else if errors.Is(err, context.DeadlineExceeded) {
		code = "timeout"
	} else if errors.Is(err, context.Canceled) {
		code = "canceled"
	}
	return &Error{Code: code, Operation: s.builder.decoder.Protocol, Protocol: s.builder.result.Protocol, Session: s.builder.result.Session, TurnID: s.builder.result.TurnID, IdempotencyKey: s.builder.decoder.IdempotencyKey, ClientRef: s.builder.decoder.ClientRef, Cause: err}
}

func (s *RunStream) drain() {
	for {
		record, err := s.process.DrainNext()
		if err != nil {
			break
		}
		if s.parseErr != nil {
			// After a protocol failure, retain only matching cleanup confirmation;
			// no later content or terminal can turn the failed inference into success.
			if r, e := protocol.Decode(record, s.builder.decoder.Protocol, s.builder.decoder.Version); e == nil && r.Type == "terminal" && r.SessionID == s.builder.result.Session.SessionID && r.Cleanup != "" {
				s.builder.result.Cleanup = CleanupDisposition(r.Cleanup)
			}
			continue
		}
		if _, err := s.builder.add(record); err != nil {
			s.parseErr = err
			s.process.Cancel(err)
		}
	}
}

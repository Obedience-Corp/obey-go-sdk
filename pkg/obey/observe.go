package obey

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Obedience-Corp/obey-go-sdk/internal/execclient"
	"github.com/Obedience-Corp/obey-go-sdk/internal/protocol"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type EventStream struct {
	prefetched              bool
	process                 *execclient.Process
	ctx                     context.Context
	decoder                 protocol.Decoder
	protocol                string
	requested, expectedCamp string
	camp, observed          string
	filters                 map[string]bool
	from                    uint64
	timer                   *time.Timer
	active                  atomic.Bool
	mu                      sync.Mutex
	event                   Event
	err, misuse             error
	exhausted               bool
}

func (s *Session) Attach(ctx context.Context, options AttachOptions) (*EventStream, error) {
	if err := s.client.checkOpen(); err != nil {
		return nil, err
	}
	if err := validateSessionRef(s.ref); err != nil {
		return nil, scopeError(&Error{Code: "invalid_input", Operation: "session.attach", Cause: err}, TurnRef{SessionRef: s.ref})
	}
	if err := s.client.requireProtocol(ctx, "session_attach", "obey.session.attach", 1); err != nil {
		return nil, scopeError(err, TurnRef{SessionRef: s.ref})
	}
	if err := s.client.requireFeatures(ctx, "chat_surfaces"); err != nil {
		return nil, scopeError(err, TurnRef{SessionRef: s.ref})
	}
	args := []string{"session", "attach", "id:" + s.ref.SessionID, "--campaign", s.ref.CampID, "--json", "--protocol-version", "1"}
	if options.FromSequence != 0 {
		args = append(args, "--from-seq", strconv.FormatUint(options.FromSequence, 10))
	}
	if options.BackfillLimit != 0 {
		args = append(args, "--backfill-limit", strconv.FormatUint(uint64(options.BackfillLimit), 10))
	}
	if options.NoBackfill {
		args = append(args, "--no-backfill")
	}
	return s.client.startEventStream(ctx, args, "obey.session.attach", s.ref.CampID, s.ref.SessionID, nil, options.FromSequence)
}
func (c *Client) Observe(ctx context.Context, options ObserveOptions) (*EventStream, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	options.SessionIDs = append([]string(nil), options.SessionIDs...)
	camp := options.Camp
	if camp == "" {
		camp = c.config.camp
	}
	if camp == "" {
		camp = c.config.defaults.Camp
	}
	if strings.TrimSpace(camp) == "" || strings.ContainsRune(camp, '\x00') || strings.ContainsRune(options.Cursor, '\x00') {
		return nil, &Error{Code: "invalid_input", Operation: "observe", Cause: errors.New("explicit camp and valid cursor required")}
	}
	filters := map[string]bool{}
	for _, id := range options.SessionIDs {
		if strings.TrimSpace(id) == "" || strings.ContainsRune(id, '\x00') {
			return nil, &Error{Code: "invalid_input", Operation: "observe", Cause: errors.New("session filters must be exact nonempty IDs")}
		}
		filters[id] = true
	}
	tail := options.Tail
	if tail == 0 && !options.Follow {
		tail = 20
	}
	if options.Follow && tail == 0 && options.Cursor != "" {
		return nil, &Error{Code: "invalid_options", Operation: "observe", Cause: errors.New("history cursor requires a positive tail")}
	}
	if err := c.requireProtocol(ctx, "observe", "obey.observe", 1); err != nil {
		return nil, err
	}
	if options.Follow {
		if err := c.requireFeatures(ctx, "chat_surfaces"); err != nil {
			return nil, err
		}
	}
	args := []string{"observe", "--campaign", camp, "--json", "--protocol-version", "1", "--tail", strconv.FormatUint(uint64(tail), 10)}
	if options.Follow {
		args = append(args, "--follow")
	}
	if options.Cursor != "" {
		args = append(args, "--cursor", options.Cursor)
	}
	for _, id := range options.SessionIDs {
		args = append(args, "--session", id)
	}
	return c.startEventStream(ctx, args, "obey.observe", camp, "", filters, 0)
}
func (c *Client) startEventStream(ctx context.Context, args []string, name, camp, requested string, filters map[string]bool, from uint64) (*EventStream, error) {
	streamCtx, cancel := context.WithCancelCause(ctx)
	process, err := c.start(streamCtx, args, nil)
	if err != nil {
		cancel(nil)
		return nil, scopeError(err, TurnRef{SessionRef: SessionRef{CampID: camp, SessionID: requested}})
	}
	stream := &EventStream{process: process, ctx: ctx, decoder: protocol.Decoder{Protocol: name, Version: 1}, protocol: name, expectedCamp: camp, requested: requested, filters: filters, from: from}
	// The CLI has its own establishment bound; this also covers a nonresponsive
	// binary before its first valid packet without shortening stream lifetime.
	stream.timer = time.AfterFunc(unaryTimeout, func() { process.Cancel(context.DeadlineExceeded) })
	// The runner owns child cancellation/reaping. Release this lightweight parent
	// context when that process is done, without consuming any record.
	go func() { process.Wait(); stream.timer.Stop(); cancel(nil) }()
	if !stream.Next() {
		err := stream.Err()
		_ = stream.Close()
		if enriched := stream.Err(); enriched != nil {
			err = enriched
		}
		return nil, err
	}
	stream.mu.Lock()
	stream.prefetched = true
	stream.mu.Unlock()
	return stream, nil
}
func (s *EventStream) Next() bool {
	if !s.active.CompareAndSwap(false, true) {
		s.mu.Lock()
		s.misuse = &Error{Code: "stream_misuse", Operation: s.protocol, Cause: errors.New("Next requires one consumer")}
		s.mu.Unlock()
		return false
	}
	defer s.active.Store(false)
	s.mu.Lock()
	exhausted := s.exhausted
	if !exhausted && s.prefetched {
		s.prefetched = false
		s.mu.Unlock()
		return true
	}
	s.mu.Unlock()
	if exhausted {
		return false
	}
	data, err := s.process.Next(s.ctx)
	if err != nil {
		s.finish(err)
		return false
	}
	var event Event
	// Next is the sole decoder owner. Scope is committed together with payload,
	// so malformed/foreign frames cannot replace the last trusted identities.
	record, err := s.decoder.AddWithValidation(data, func(r protocol.Envelope) (int64, error) {
		var err error
		event, err = projectRecord(r)
		if err != nil {
			return 0, err
		}
		return 0, s.validateScope(r, event)
	})
	_ = record
	if err != nil {
		s.process.Cancel(err)
		s.mu.Lock()
		s.err = s.publicError(err, data)
		s.exhausted = true
		s.mu.Unlock()
		return false
	}
	s.timer.Stop()
	s.mu.Lock()
	s.camp = event.Session.CampID
	if event.ObservedSessionID != "" {
		s.observed = event.ObservedSessionID
	}
	s.event = event
	s.mu.Unlock()
	return true
}
func (s *EventStream) validateScope(r protocol.Envelope, event Event) error {
	s.mu.Lock()
	camp := s.camp
	s.mu.Unlock()
	if camp == "" && fullUUID(s.expectedCamp) {
		camp = s.expectedCamp
	}
	if r.CampID == "" || (camp != "" && r.CampID != camp) {
		return &Error{Code: "scope_mismatch", Cause: errors.New("observation camp differs from requested scope")}
	}
	if s.requested != "" {
		if r.RequestedSessionID != s.requested {
			return &Error{Code: "scope_mismatch", Cause: errors.New("attach requested session differs")}
		}
		if r.Type == "event" && (r.ObservedSessionID == "" || r.SessionID != r.ObservedSessionID) {
			return &Error{Code: "scope_mismatch", Cause: errors.New("attach observed identity metadata is missing or inconsistent")}
		}
		if s.from > 0 && r.ObservedSessionID != "" && r.ObservedSessionID != s.requested {
			return &Error{Code: "scope_mismatch", RequestedSessionID: s.requested, ObservedSessionID: r.ObservedSessionID, Cause: errors.New("durable cursor belongs to original session; reset explicitly after redirect")}
		}
	}
	if len(s.filters) > 0 {
		if event.Session.SessionID != "" && !s.filters[event.Session.SessionID] {
			return &Error{Code: "scope_mismatch", Cause: errors.New("observed session is outside explicit filters")}
		}
		if event.History != nil {
			for _, row := range event.History.Entries {
				if !s.filters[row.Session.SessionID] {
					return &Error{Code: "scope_mismatch", Cause: errors.New("history session is outside explicit filters")}
				}
			}
		}
	}
	return nil
}
func (s *EventStream) finish(readErr error) {
	s.timer.Stop()
	var cause error = readErr
	var outcome *execclient.Outcome
	if s.process.Finished() || readErr == io.EOF {
		out := s.process.Wait()
		outcome = &out
		cause = errors.Join(readErr, out.Err)
		decoded := s.decoder.Finish(out.ExitCode)
		if decoded == nil && (out.Err == nil || (out.ExitCode == 0 && contextOnly(out.Err))) {
			s.mu.Lock()
			s.exhausted = true
			s.mu.Unlock()
			return
		}
		if decoded != nil {
			cause = errors.Join(decoded, cause)
		}
	}
	s.mu.Lock()
	s.err = s.publicError(cause, nil)
	if outcome != nil {
		s.enrichOutcome(*outcome)
	}
	s.exhausted = true
	s.mu.Unlock()
}

// publicError is called with mu held so scope/error snapshots remain race-safe.
func (s *EventStream) publicError(err error, raw []byte) *Error {
	code := "protocol_error"
	var pe *protocol.Error
	var known *Error
	if errors.As(err, &known) {
		code = known.Code
	}
	if errors.As(err, &pe) {
		code = pe.Code
	}
	if errors.Is(err, execclient.ErrOutputLimit) {
		code = "output_limit"
	} else if errors.Is(err, context.DeadlineExceeded) {
		code = "timeout"
	} else if errors.Is(err, context.Canceled) {
		code = "canceled"
	}
	if code == "canceled" {
		err = errors.Join(err, context.Canceled)
	}
	if code == "timeout" {
		err = errors.Join(err, context.DeadlineExceeded)
	}
	e := &Error{Code: code, Operation: s.protocol, Protocol: ProtocolVersion{Name: s.protocol, Version: 1}, Session: SessionRef{CampID: s.camp, SessionID: s.requested}, RequestedSessionID: s.requested, ObservedSessionID: s.observed, Cause: err, RawRecord: append(json.RawMessage(nil), raw...)}
	if e.Session.CampID == "" {
		e.Session.CampID = s.expectedCamp
	}
	if known != nil {
		e.RequestedSessionID = known.RequestedSessionID
		if e.RequestedSessionID == "" {
			e.RequestedSessionID = s.requested
		}
		if known.ObservedSessionID != "" {
			e.ObservedSessionID = known.ObservedSessionID
		}
	}
	if pe != nil && pe.Record != nil {
		r := pe.Record
		e.RawRecord = append(json.RawMessage(nil), r.Raw...)
		e.DaemonCode = ptrCopy(r.DaemonCode)
		e.GRPCCode = r.GRPCCode
		e.Details = append(json.RawMessage(nil), r.Details...)
		e.TurnID = r.TurnID
		if s.requested == "" && r.SessionID != "" {
			e.Session.SessionID = r.SessionID
		}
		if r.RequestedSessionID != "" {
			e.RequestedSessionID = r.RequestedSessionID
		}
		if r.ObservedSessionID != "" {
			e.ObservedSessionID = r.ObservedSessionID
		}
		e.TargetTurnID = r.TargetTurnID
		e.CallID = r.CallID
		e.DecisionIndeterminate = r.DecisionIndeterminate
		e.Diagnostics = []Diagnostic{{Code: r.Code, Message: r.Message, Severity: r.Severity, DaemonCode: ptrCopy(r.DaemonCode), Details: append(json.RawMessage(nil), r.Details...)}}
	}
	return e
}
func (s *EventStream) Event() Event { s.mu.Lock(); defer s.mu.Unlock(); return cloneEvent(s.event) }

// Outcome enrichment never consumes stream records or changes trusted scope.
func (s *EventStream) enrichOutcome(out execclient.Outcome) {
	if s.err == nil {
		return
	}
	var e *Error
	if !errors.As(s.err, &e) {
		return
	}
	copy := *e
	copy.Cause = errors.Join(copy.Cause, out.Err)
	copy.ExitCode = ptrCopy(&out.ExitCode)
	copy.StderrTail = out.StderrTail
	if errors.Is(out.Err, execclient.ErrOutputLimit) && (copy.Code == "canceled" || copy.Code == "timeout") {
		copy.Code = "output_limit"
	}
	s.err = &copy
}
func (s *EventStream) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.misuse != nil {
		return cloneError(s.misuse)
	}
	if s.process.Finished() {
		s.enrichOutcome(s.process.Wait())
	}
	return cloneError(s.err)
}
func (s *EventStream) Close() error {
	s.timer.Stop()
	err := s.process.Close()
	out := s.process.Wait()
	s.mu.Lock()
	s.enrichOutcome(out)
	s.mu.Unlock()
	return err
}

package obey

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/obediencecorp/obey-go-sdk/internal/protocol"
	"strconv"
	"strings"
	"time"
)

func (s *Session) Transcript(ctx context.Context, options TranscriptOptions) (*TranscriptPage, error) {
	if err := s.client.checkOpen(); err != nil {
		return nil, err
	}
	return s.transcript(ctx, options, false)
}
func (s *Session) transcript(ctx context.Context, options TranscriptOptions, newest bool) (*TranscriptPage, error) {
	ref := TurnRef{SessionRef: s.ref}
	if err := validateSessionRef(s.ref); err != nil {
		return nil, scopeError(&Error{Code: "invalid_input", Operation: "session.transcript", Cause: err}, ref)
	}
	if strings.ContainsRune(options.Cursor, '\x00') {
		return nil, scopeError(&Error{Code: "invalid_input", Operation: "session.transcript", Cause: errors.New("cursor contains NUL")}, ref)
	}
	args := []string{"session", "transcript", "id:" + s.ref.SessionID}
	if options.PageSize != 0 {
		args = append(args, "--limit", strconv.FormatUint(uint64(options.PageSize), 10))
	}
	if options.Cursor != "" {
		args = append(args, "--cursor", options.Cursor)
	}
	if newest {
		args = append(args, "--newest-first")
	}
	data, err := s.client.readJSON(ctx, args, "session.transcript", ref)
	if err != nil {
		return nil, err
	}
	page, err := decodeTranscriptPage(data, s.ref)
	if err != nil {
		return nil, scopeError(err, ref)
	}
	return page, nil
}
func decodeTranscriptPage(data []byte, ref SessionRef) (*TranscriptPage, error) {
	var doc protocol.TranscriptDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, &Error{Code: "protocol_error", Operation: "session.transcript", Cause: err}
	}
	page := &TranscriptPage{NextCursor: doc.Page.NextCursor, HasMore: doc.Page.HasMore, Raw: append(json.RawMessage(nil), data...)}
	for _, raw := range doc.Entries {
		var row protocol.TranscriptRow
		if err := json.Unmarshal(raw, &row); err != nil {
			return nil, &Error{Code: "protocol_error", Operation: "session.transcript", Cause: err}
		}
		if row.SessionID != ref.SessionID || row.CampID != ref.CampID {
			return nil, &Error{Code: "scope_mismatch", Operation: "session.transcript", RawRecord: append(json.RawMessage(nil), raw...), Cause: errors.New("transcript entry differs from requested camp/session")}
		}
		if row.ID == "" || row.Sequence == 0 {
			return nil, &Error{Code: "protocol_error", Operation: "session.transcript", Cause: errors.New("transcript entry lacks durable identity/sequence")}
		}
		entry := TranscriptEntry{ID: row.ID, Session: ref, Sequence: uint64(row.Sequence), Actor: row.Actor, Kind: row.Kind, Content: row.Content, ToolName: row.ToolName, ToolCallID: row.ToolCallID, Success: ptrCopy(row.Success), Error: row.Error, TurnID: row.TurnID, AttemptID: row.AttemptID, Raw: append(json.RawMessage(nil), raw...)}
		if row.Timestamp != "" {
			value, err := time.Parse(time.RFC3339Nano, row.Timestamp)
			if err != nil {
				return nil, &Error{Code: "protocol_error", Operation: "session.transcript", Cause: err}
			}
			entry.Timestamp = &value
		}
		if len(row.ActivityType) > 0 {
			if json.Unmarshal(row.ActivityType, &entry.ActivityType) != nil {
				entry.ActivityType = string(row.ActivityType)
			}
		}
		if row.Metadata != "" {
			if json.Valid([]byte(row.Metadata)) {
				entry.Metadata = json.RawMessage(row.Metadata)
			} else {
				entry.Metadata, _ = json.Marshal(row.Metadata)
			}
		}
		page.Entries = append(page.Entries, entry)
	}
	return page, nil
}

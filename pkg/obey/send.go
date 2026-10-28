package obey

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/obediencecorp/obey-go-sdk/internal/protocol"
)

const maxSendBytes = 6 << 20
const maxInlineFileBytes = 3 << 20
const maxImageFileBytes = 10 << 20
const maxPDFFileBytes = 20 << 20
const maxSendFiles = 8

func cloneSendOptions(options *SendOptions) (out SendOptions) {
	if options != nil {
		out = *options
		out.Files = append([]string(nil), options.Files...)
	}
	return out
}
func prepareSendOptions(options *SendOptions) (SendOptions, error) {
	out := cloneSendOptions(options)
	if out.IdempotencyKey == "" {
		var bytes [16]byte
		if _, err := rand.Read(bytes[:]); err != nil {
			return out, err
		}
		out.IdempotencyKey = hex.EncodeToString(bytes[:])
	}
	if len(out.IdempotencyKey) > 128 || strings.TrimSpace(out.IdempotencyKey) != out.IdempotencyKey || strings.ContainsRune(out.IdempotencyKey, '\x00') {
		return out, errors.New("invalid idempotency key: at most 128 bytes, no edge whitespace/NUL")
	}
	if len(out.ClientRef) > 64 || strings.ContainsRune(out.ClientRef, '\x00') {
		return out, errors.New("client reference must fit 64 bytes without NUL")
	}
	if strings.ContainsRune(out.Model, '\x00') || (out.Model != "" && strings.TrimSpace(out.Model) == "") {
		return out, errors.New("invalid model override")
	}
	switch out.Mode {
	case "", "autonomous", "supervised", "plan", "discussion", "fest":
	default:
		if !strings.HasPrefix(out.Mode, "vendor:") {
			return out, errors.New("unsupported send mode; use the explicit control API for stop")
		}
		parts := strings.SplitN(strings.TrimPrefix(out.Mode, "vendor:"), "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.ContainsRune(out.Mode, '\x00') {
			return out, errors.New("invalid vendor mode")
		}
	}
	return out, nil
}
func validateSendFiles(ctx context.Context, prompt string, files []string, cwd string) error {
	if strings.TrimSpace(prompt) == "" {
		return errors.New("prompt is required")
	}
	if len(prompt) > maxSendBytes {
		return errors.New("text exceeds 6 MiB daemon limit")
	}
	if len(files) > maxSendFiles {
		return errors.New("at most 8 attachments are supported")
	}
	combined := int64(len(prompt))
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		resolved := path
		if !filepath.IsAbs(path) && cwd != "" {
			resolved = filepath.Join(cwd, path)
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return fmt.Errorf("stat attachment %q: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("attachment %q is not a regular file", path)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		file, err := os.Open(resolved)
		if err != nil {
			return fmt.Errorf("open attachment %q: %w", path, err)
		}
		size, err := inspectSendFile(file)
		_ = file.Close()
		if err != nil {
			return fmt.Errorf("validate attachment %q: %w", path, err)
		}
		if size <= maxInlineFileBytes {
			combined += size
		}
		if combined > maxSendBytes {
			return errors.New("text and inline attachments exceed6 MiB daemon limit")
		}
	}
	return ctx.Err()
}
func inspectSendFile(file *os.File) (int64, error) {
	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, errors.New("attachment is not a regular file")
	}
	if info.Size() > maxPDFFileBytes {
		return 0, errors.New("attachment exceeds 20 MiB upload limit")
	}
	var header [8]byte
	n, err := io.ReadFull(file, header[:])
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return 0, err
	}
	limit := int64(maxImageFileBytes)
	switch {
	case n >= 8 && string(header[:8]) == "\x89PNG\r\n\x1a\n":
	case n >= 3 && header[0] == 0xff && header[1] == 0xd8 && header[2] == 0xff:
	case n >= 4 && string(header[:4]) == "%PDF":
		limit = maxPDFFileBytes
	default:
		return 0, errors.New("attachment must be PNG, JPEG or PDF")
	}
	if info.Size() > limit {
		return 0, errors.New("attachment exceeds MIME-specific upload limit")
	}
	return info.Size(), nil
}
func (s *Session) Send(ctx context.Context, prompt string, options *SendOptions) (*Turn, error) {
	if err := s.client.checkOpen(); err != nil {
		return nil, err
	}
	opts, err := prepareSendOptions(options)
	ref := TurnRef{SessionRef: s.ref, IdempotencyKey: opts.IdempotencyKey, ClientRef: opts.ClientRef}
	if err == nil {
		err = validateSessionRef(s.ref)
	}
	if err == nil {
		err = validateSendFiles(ctx, prompt, opts.Files, s.client.config.cwd)
	}
	if err != nil {
		code := "invalid_input"
		if errors.Is(err, context.Canceled) {
			code = "canceled"
		}
		if errors.Is(err, context.DeadlineExceeded) {
			code = "timeout"
		}
		return nil, scopeError(&Error{Code: code, Operation: "session.send", Cause: err}, ref)
	}
	if err := s.client.requireProtocol(ctx, "session_send", "obey.session.send", 1); err != nil {
		return nil, scopeError(err, ref)
	}
	features := []string{"chat_surfaces", "durable_turns", "send_acceptance"}
	if opts.Model != "" {
		features = append(features, "per_turn_model")
	}
	if err := s.client.requireFeatures(ctx, features...); err != nil {
		return nil, scopeError(err, ref)
	}
	args := []string{"session", "send", s.ref.SessionID, "--campaign", s.ref.CampID, "--input", "-", "--json", "--protocol-version", "1", "--idempotency-key", opts.IdempotencyKey}
	for _, pair := range [][2]string{{"--client-ref", opts.ClientRef}, {"--mode", opts.Mode}, {"--model", opts.Model}} {
		if pair[1] != "" {
			args = append(args, pair[0], pair[1])
		}
	}
	for _, path := range opts.Files {
		args = append(args, "--file", path)
	}
	records, callErr := s.client.machineDocumentInput(ctx, args, strings.NewReader(prompt), "session.send", "obey.session.send", ref)
	var receipt *Acceptance
	for _, record := range records {
		if record.Type != "event" || record.PayloadType != "local.v1.SendMessageResponse" {
			continue
		}
		identity := protocol.ReceiptIdentity(record, s.ref.CampID, s.ref.SessionID, ref.IdempotencyKey, ref.ClientRef)
		if identity == "" {
			continue
		}
		ref.TurnID = identity
		parsed, err := protocol.DecodeAcceptance(record.Payload)
		if err != nil || record.TurnID != identity {
			if err == nil {
				err = errors.New("receipt envelope turn scope missing")
			}
			if callErr == nil {
				callErr = &Error{Code: "protocol_error", Operation: "session.send", RawRecord: append(json.RawMessage(nil), record.Raw...), Cause: err}
			}
			continue
		}
		position := ptrCopy(parsed.QueuePosition)
		if parsed.Status == string(TurnQueued) {
			if position == nil {
				position = new(uint32)
			}
		} else {
			position = nil
		}
		receipt = &Acceptance{Ref: ref, Status: TurnStatus(parsed.Status), QueuePosition: position, Deduplicated: parsed.Deduplicated, Raw: append(json.RawMessage(nil), parsed.Raw...)}
	}
	if callErr == nil {
		if receipt == nil || len(records) == 0 || records[len(records)-1].Outcome != "accepted" {
			callErr = &Error{Code: "indeterminate_acceptance", Operation: "session.send", Cause: errors.New("acceptance-only result missing")}
		}
	}
	if callErr != nil {
		return s.sendFailure(ref, receipt, callErr)
	}
	return &Turn{client: s.client, ref: ref, acceptance: receipt}, nil
}
func (s *Session) sendFailure(ref TurnRef, receipt *Acceptance, cause error) (*Turn, error) {
	var known *Error
	if ref.TurnID == "" && errors.As(cause, &known) {
		ref.TurnID = known.TurnID
	}
	scoped := scopeError(cause, ref)
	var failure *Error
	_ = errors.As(scoped, &failure)
	if ref.TurnID == "" && failure.TurnID != "" {
		ref.TurnID = failure.TurnID
	}
	if ref.TurnID == "" && len(failure.RawRecord) > 0 {
		if record, err := protocol.Decode(failure.RawRecord, "obey.session.send", 1); err == nil {
			ref.TurnID = protocol.ReceiptIdentity(record, ref.CampID, ref.SessionID, ref.IdempotencyKey, ref.ClientRef)
		}
	}
	failure.TurnID = ref.TurnID
	trusted := failure.validatedCLIRecord
	if trusted {
		record, err := protocol.Decode(failure.RawRecord, "obey.session.send", 1)
		trusted = err == nil && record.Type == "error" && record.Code == failure.Code && record.CampID == ref.CampID && record.SessionID == ref.SessionID && (record.IdempotencyKey == "" || record.IdempotencyKey == ref.IdempotencyKey) && (record.ClientRef == "" || record.ClientRef == ref.ClientRef)
	}
	definite := failure.ExitCode == nil && (failure.Operation == "start" || failure.Code == "closed_client")
	if trusted {
		switch failure.Code {
		case "invalid_input", "invalid_options", "unsupported_protocol", "unsupported_capability", "daemon_unavailable", "daemon_error", "not_found", "scope_mismatch", "undelivered_note":
			definite = true
		}
	}
	if ref.TurnID != "" || failure.AcceptanceIndeterminate {
		definite = false
	}
	if !definite {
		original := failure.Code
		failure.AcceptanceIndeterminate = true
		// Unknown validated CLI codes remain original; uncertainty is explicit. Wire
		// or transport failure cannot impersonate a same-named pre-submission error.
		if !trusted || knownSendFailureCode(original) {
			failure.Code = "indeterminate_acceptance"
		}
		if original != failure.Code {
			failure.Diagnostics = append(failure.Diagnostics, Diagnostic{Code: original, Message: cause.Error(), Severity: "error", Details: append(json.RawMessage(nil), failure.RawRecord...)})
		}
	}
	if ref.TurnID == "" {
		return nil, failure
	}
	turn := &Turn{client: s.client, ref: ref, acceptance: receipt}
	return turn, failure
}

func knownSendFailureCode(code string) bool {
	switch code {
	case "protocol_error", "incomplete_stream", "timeout", "canceled", "command_error", "output_limit", "invalid_input", "invalid_options", "unsupported_protocol", "unsupported_capability", "daemon_unavailable", "daemon_error", "not_found", "scope_mismatch", "undelivered_note", "indeterminate_acceptance":
		return true
	}
	return false
}

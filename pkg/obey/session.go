package obey

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

type Session struct {
	client *Client
	ref    SessionRef
}

func (s *Session) Ref() SessionRef { return s.ref }
func (c *Client) OpenSession(ctx context.Context, ref SessionRef) (*Session, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	if err := validateSessionRef(ref); err != nil {
		return nil, scopeError(&Error{Code: "invalid_input", Operation: "session.open", Cause: err}, TurnRef{SessionRef: ref})
	}
	data, err := c.readJSON(ctx, []string{"session", "info", "id:" + ref.SessionID}, "session.info", TurnRef{SessionRef: ref})
	if err != nil {
		return nil, err
	}
	var response struct {
		Session struct {
			ID     string `json:"id"`
			CampID string `json:"campaign_id"`
		} `json:"session"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, scopeError(&Error{Code: "protocol_error", Operation: "session.open", Cause: err}, TurnRef{SessionRef: ref})
	}
	if response.Session.ID != ref.SessionID || response.Session.CampID != ref.CampID {
		return nil, scopeError(&Error{Code: "scope_mismatch", Operation: "session.open", RawRecord: append(json.RawMessage(nil), data...), Cause: errors.New("returned session scope differs from requested identity")}, TurnRef{SessionRef: ref})
	}
	return &Session{client: c, ref: ref}, nil
}
func (c *Client) CreateSession(ctx context.Context, options *SessionOptions) (*Session, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	opts := cloneSessionOptions(c.config.defaults)
	if options != nil {
		opts = mergeSession(opts, cloneSessionOptions(*options))
	}
	if (options == nil || options.Camp == "") && c.config.camp != "" {
		opts.Camp = c.config.camp
	}
	ref := TurnRef{SessionRef: SessionRef{CampID: opts.Camp}}
	if strings.TrimSpace(opts.Camp) == "" || strings.TrimSpace(opts.Provider) == "" || strings.TrimSpace(opts.Model) == "" {
		return nil, scopeError(&Error{Code: "invalid_input", Operation: "session.create", Cause: errors.New("explicit camp/provider/model required")}, ref)
	}
	if err := validateConfig(opts.Config); err != nil {
		return nil, scopeError(&Error{Code: "invalid_input", Operation: "session.create", Cause: err}, ref)
	}
	if err := c.requireProtocol(ctx, "session_create", "obey.session.create", 1); err != nil {
		return nil, scopeError(err, ref)
	}
	args := []string{"session", "create", "--campaign", opts.Camp, "--provider", opts.Provider, "--model", opts.Model, "--json", "--protocol-version", "1"}
	fields := []struct{ flag, value string }{{"--workdir", opts.WorkingDirectory}, {"--agent", opts.Agent}, {"--agent-profile", opts.AgentProfile}, {"--mode", opts.Mode}, {"--reasoning-effort", opts.ReasoningEffort}, {"--festival", opts.FestivalID}, {"--task", opts.TaskID}}
	for _, field := range fields {
		if field.value != "" {
			args = append(args, field.flag, field.value)
		}
	}
	if opts.Config != nil {
		args = append(args, "--config", string(opts.Config))
	}
	records, err := c.machineDocument(ctx, args, "session.create", "obey.session.create", ref)
	if err != nil {
		var typed *Error
		if errors.As(err, &typed) && typed.Session.SessionID != "" {
			return &Session{client: c, ref: typed.Session}, err
		}
		return nil, err
	}
	for _, record := range records {
		if record.Type == "session_created" {
			return &Session{client: c, ref: SessionRef{CampID: record.CampID, SessionID: record.SessionID}}, nil
		}
	}
	return nil, scopeError(&Error{Code: "indeterminate_creation", Operation: "session.create", Cause: errors.New("creation succeeded without identity")}, ref)
}

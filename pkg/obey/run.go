package obey

import (
	"context"
	"errors"
	"strings"
)

func (c *Client) prepareRun(options *RunOptions) (RunOptions, error) {
	var out RunOptions
	if options != nil {
		out = *options
		out.Files = append([]string(nil), options.Files...)
		if options.Timeout != nil {
			value := *options.Timeout
			out.Timeout = &value
		}
	}
	out.SessionOptions = mergeSession(c.config.defaults, out.SessionOptions)
	if options == nil || options.Camp == "" {
		if c.config.camp != "" {
			out.Camp = c.config.camp
		}
	}
	if strings.TrimSpace(out.Provider) == "" || strings.TrimSpace(out.Model) == "" {
		return out, errors.New("provider and model are required")
	}
	if out.Timeout != nil && *out.Timeout < 0 {
		return out, errors.New("negative timeout")
	}
	if err := validateConfig(out.Config); err != nil {
		return out, err
	}
	return out, nil
}
func runArgs(options RunOptions) []string {
	args := []string{"run", "--input", "-", "--json", "--provider", options.Provider, "--model", options.Model}
	for _, pair := range [][2]string{{"--campaign", options.Camp}, {"--workdir", options.WorkingDirectory}, {"--agent", options.Agent}, {"--agent-profile", options.AgentProfile}, {"--mode", options.Mode}, {"--reasoning-effort", options.ReasoningEffort}, {"--festival", options.FestivalID}, {"--task", options.TaskID}, {"--config", string(options.Config)}} {
		if pair[1] != "" {
			args = append(args, pair[0], pair[1])
		}
	}
	for _, file := range options.Files {
		args = append(args, "--file", file)
	}
	if options.Timeout != nil {
		args = append(args, "--timeout", options.Timeout.String())
	}
	return args
}
func (c *Client) StreamPrompt(ctx context.Context, prompt string, options *RunOptions) (*RunStream, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	opts, err := c.prepareRun(options)
	if err != nil {
		return nil, &Error{Code: "invalid_options", Operation: "run", Cause: err}
	}
	if strings.TrimSpace(prompt) == "" {
		return nil, &Error{Code: "invalid_input", Operation: "run", Cause: errors.New("prompt is required")}
	}
	caps, err := c.Capabilities(ctx)
	if err != nil {
		return nil, err
	}
	version := 1
	requireReceipt := false
	if opts.Camp != "" && supports(caps, "session_run", "obey.session.run", 2) {
		status, statusErr := c.Status(ctx)
		if statusErr != nil {
			var typed *Error
			if !errors.As(statusErr, &typed) || typed.Code != "unsupported_protocol" {
				return nil, statusErr
			}
		} else if status.Features["chat_surfaces"] && status.Features["durable_turns"] && status.Features["turn_identity"] {
			version = 2
			requireReceipt = status.Features["send_acceptance"]
		}
	}
	if opts.RequireLiveEvents && version != 2 {
		return nil, &Error{Code: "unsupported_capability", Operation: "run", Cause: errors.New("live run requires explicit camp and compatible CLI/daemon capabilities")}
	}
	if !supports(caps, "session_run", "obey.session.run", version) {
		return nil, &Error{Code: "unsupported_protocol", Operation: "run"}
	}
	if version == 2 {
		if err := validateSendFiles(ctx, prompt, opts.Files, c.config.cwd); err != nil {
			code := "invalid_input"
			if errors.Is(err, context.Canceled) {
				code = "canceled"
			}
			if errors.Is(err, context.DeadlineExceeded) {
				code = "timeout"
			}
			return nil, &Error{Code: code, Operation: "run", Cause: err}
		}
	}
	args := runArgs(opts)
	fidelity := FidelityFinalOnly
	if version == 2 {
		args = append(args, "--protocol-version", "2")
		fidelity = FidelityLive
	}
	p, err := c.start(ctx, args, strings.NewReader(prompt))
	if err != nil {
		return nil, err
	}
	builder := newResultBuilder("obey.session.run", version, c.config.limits.MaxResultBytes)
	builder.decoder.RequireAcceptance = requireReceipt
	return &RunStream{process: p, ctx: ctx, builder: builder, fidelity: fidelity}, nil
}
func (c *Client) RunPrompt(ctx context.Context, prompt string, options *RunOptions) (*Result, error) {
	s, err := c.StreamPrompt(ctx, prompt, options)
	if err != nil {
		return nil, err
	}
	return s.Result()
}

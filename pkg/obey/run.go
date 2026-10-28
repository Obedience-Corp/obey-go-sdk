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
	if !supports(caps, "session_run", "obey.session.run", 1) {
		return nil, &Error{Code: "unsupported_protocol", Operation: "run"}
	}
	if opts.RequireLiveEvents {
		return nil, &Error{Code: "unsupported_capability", Operation: "run", Cause: errors.New("run-v1 is final-only")}
	}
	p, err := c.start(ctx, runArgs(opts), strings.NewReader(prompt))
	if err != nil {
		return nil, err
	}
	return &RunStream{process: p, ctx: ctx, builder: newResultBuilder("obey.session.run", 1, c.config.limits.MaxResultBytes), fidelity: FidelityFinalOnly}, nil
}
func (c *Client) RunPrompt(ctx context.Context, prompt string, options *RunOptions) (*Result, error) {
	s, err := c.StreamPrompt(ctx, prompt, options)
	if err != nil {
		return nil, err
	}
	return s.Result()
}

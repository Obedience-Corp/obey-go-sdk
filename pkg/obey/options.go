package obey

import (
	"encoding/json"
	"errors"
	"strings"
)

type ClientOption func(*clientConfig) error
type clientConfig struct {
	socket, camp, cwd string
	defaults          SessionOptions
	env               map[string]string
	limits            Limits
}

func WithSocket(path string) ClientOption {
	return func(c *clientConfig) error { c.socket = path; return nil }
}
func WithCamp(camp string) ClientOption {
	return func(c *clientConfig) error { c.camp = camp; return nil }
}
func WithWorkingDirectory(path string) ClientOption {
	return func(c *clientConfig) error { c.cwd = path; return nil }
}
func WithSessionDefaults(options SessionOptions) ClientOption {
	copy := cloneSessionOptions(options)
	return func(c *clientConfig) error { c.defaults = cloneSessionOptions(copy); return nil }
}
func WithEnvironment(overrides map[string]string) ClientOption {
	copy := make(map[string]string, len(overrides))
	for k, v := range overrides {
		copy[k] = v
	}
	return func(c *clientConfig) error {
		c.env = make(map[string]string, len(copy))
		for k, v := range copy {
			if k == "" || strings.ContainsAny(k, "=\x00") || strings.ContainsRune(v, '\x00') {
				return errors.New("invalid environment override")
			}
			c.env[k] = v
		}
		return nil
	}
}
func WithLimits(limits Limits) ClientOption {
	return func(c *clientConfig) error { c.limits = limits; return nil }
}
func cloneSessionOptions(s SessionOptions) SessionOptions {
	s.Config = append(json.RawMessage(nil), s.Config...)
	return s
}
func validateConfig(config json.RawMessage) error {
	if len(config) == 0 {
		return nil
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(config, &value); err != nil {
		return err
	}
	if value == nil {
		return errors.New("config must be a JSON object")
	}
	return nil
}
func mergeSession(base, override SessionOptions) SessionOptions {
	// Each field has a stable CLI meaning. Empty strings select copied defaults.
	values := []struct {
		dst *string
		src string
	}{{&base.Provider, override.Provider}, {&base.Model, override.Model}, {&base.Camp, override.Camp}, {&base.WorkingDirectory, override.WorkingDirectory}, {&base.Agent, override.Agent}, {&base.AgentProfile, override.AgentProfile}, {&base.Mode, override.Mode}, {&base.ReasoningEffort, override.ReasoningEffort}, {&base.FestivalID, override.FestivalID}, {&base.TaskID, override.TaskID}}
	for _, v := range values {
		if v.src != "" {
			*v.dst = v.src
		}
	}
	if override.Config != nil {
		base.Config = append(json.RawMessage(nil), override.Config...)
	} else {
		base.Config = append(json.RawMessage(nil), base.Config...)
	}
	return base
}

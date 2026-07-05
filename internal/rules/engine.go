package rules

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/shadow-code/shadow-mcp/internal/config"
)

// Engine matches and chains configured rules around tool calls.
type Engine struct {
	rules   []config.Rule
	runners map[string]Runner // language -> runner
}

// NewEngine builds an Engine from the rules configured in cfg.Rules and a set
// of runners keyed by rule language ("js", "python").
func NewEngine(cfgRules []config.Rule, runners map[string]Runner) *Engine {
	return &Engine{rules: cfgRules, runners: runners}
}

// RunHook runs every rule applicable to (hook, serverName, exposedName), in
// ascending priority order, chaining each rule's mutation into the next.
//
// value is the current arguments (pre-hook) or result (post-hook); the return
// value is the final mutated value after every applicable rule has run, unless
// rejected is true, in which case reason explains why and value should be
// ignored by the caller. fired lists the names of every rule that actually
// ran (including one that errored or rejected), for status/debugging display.
func (e *Engine) RunHook(ctx context.Context, hook Hook, clientProfile, serverName, toolName, exposedName string, value any) (final any, rejected bool, reason string, fired []string, err error) {
	final = value

	for _, rule := range e.applicable(hook, serverName, exposedName) {
		rule := rule
		fired = append(fired, rule.Name)

		runner, ok := e.runners[rule.Language]
		if !ok {
			return nil, false, "", fired, fmt.Errorf("rule %q: no runner registered for language %q", rule.Name, rule.Language)
		}

		input := Input{
			Hook:            hook,
			RuleName:        rule.Name,
			ClientProfile:   clientProfile,
			ServerName:      serverName,
			ToolName:        toolName,
			ExposedToolName: exposedName,
		}
		if hook == HookPre {
			input.Arguments = final
		} else {
			input.Result = final
		}

		output, runErr := runner.Run(ctx, &rule, input)
		if runErr != nil {
			if rule.OnErrorMode() == config.OnErrorSkip {
				continue
			}
			return nil, true, fmt.Sprintf("rule %q failed: %v", rule.Name, runErr), fired, nil
		}

		switch output.Action {
		case ActionReject:
			return nil, true, output.Reason, fired, nil
		case ActionContinue:
			if hook == HookPre && output.Arguments != nil {
				final = output.Arguments
			} else if hook == HookPost && output.Result != nil {
				final = output.Result
			}
		default:
			return nil, false, "", fired, fmt.Errorf("rule %q returned unknown action %q", rule.Name, output.Action)
		}
	}

	return final, false, "", fired, nil
}

func (e *Engine) applicable(hook Hook, serverName, exposedName string) []config.Rule {
	var out []config.Rule
	for _, r := range e.rules {
		if !hasHook(r.Hooks, string(hook)) {
			continue
		}
		if !appliesTo(r.AppliesTo, serverName, exposedName) {
			continue
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].EffectivePriority() < out[j].EffectivePriority()
	})
	return out
}

func hasHook(hooks []string, hook string) bool {
	for _, h := range hooks {
		if h == hook {
			return true
		}
	}
	return false
}

func appliesTo(a config.RuleAppliesTo, serverName, exposedName string) bool {
	if len(a.Servers) > 0 && !matchAny(a.Servers, serverName) {
		return false
	}
	if len(a.Tools) > 0 && !matchAny(a.Tools, exposedName) {
		return false
	}
	return true
}

func matchAny(patterns []string, name string) bool {
	for _, p := range patterns {
		if ok, _ := filepath.Match(p, name); ok {
			return true
		}
	}
	return false
}

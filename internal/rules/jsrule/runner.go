// Package jsrule executes rule scripts written in JavaScript using goja, a
// pure-Go ECMAScript interpreter with no ambient filesystem/network access.
package jsrule

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/shadow-code/shadow-mcp/internal/config"
	"github.com/shadow-code/shadow-mcp/internal/rules"
)

const defaultTimeout = 5 * time.Second

// Runner runs JS rule scripts. Each script is compiled once and cached; every
// call gets a fresh goja.Runtime (cheap, no OS process) so calls never share
// mutable state, with a hard wall-clock timeout enforced via Runtime.Interrupt.
//
// goja has no builtin filesystem/network/subprocess access, so this is a
// "trusted operator writes their own rules" execution model, not a sandbox
// for untrusted third-party scripts.
type Runner struct {
	mu       sync.Mutex
	programs map[string]*goja.Program
}

// NewRunner creates an empty JS rule runner.
func NewRunner() *Runner {
	return &Runner{programs: make(map[string]*goja.Program)}
}

// Run implements rules.Runner.
func (r *Runner) Run(ctx context.Context, rule *config.Rule, input rules.Input) (rules.Output, error) {
	program, err := r.compile(rule.Script)
	if err != nil {
		return rules.Output{}, err
	}

	vm := goja.New()
	vm.Set("console", map[string]any{
		"log": func(args ...any) {
			log.Println(append([]any{"[rule " + rule.Name + "]"}, args...)...)
		},
	})

	type result struct {
		output rules.Output
		err    error
	}
	done := make(chan result, 1)

	go func() {
		if _, err := vm.RunProgram(program); err != nil {
			done <- result{err: fmt.Errorf("running rule script %s: %w", rule.Script, err)}
			return
		}

		onCall, ok := goja.AssertFunction(vm.Get("onCall"))
		if !ok {
			done <- result{err: fmt.Errorf("rule script %s must define a top-level onCall(input) function", rule.Script)}
			return
		}

		inputVal, err := toJSValue(input)
		if err != nil {
			done <- result{err: err}
			return
		}

		val, err := onCall(goja.Undefined(), vm.ToValue(inputVal))
		if err != nil {
			done <- result{err: fmt.Errorf("rule script %s: %w", rule.Script, err)}
			return
		}

		output, err := parseOutput(val.Export())
		done <- result{output: output, err: err}
	}()

	timeout := rule.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	select {
	case res := <-done:
		return res.output, res.err
	case <-ctx.Done():
		vm.Interrupt("shadow-mcp: request cancelled")
		<-done
		return rules.Output{}, ctx.Err()
	case <-time.After(timeout):
		vm.Interrupt(fmt.Sprintf("rule %s timed out after %s", rule.Name, timeout))
		<-done
		return rules.Output{}, fmt.Errorf("rule %q timed out after %s", rule.Name, timeout)
	}
}

func (r *Runner) compile(scriptPath string) (*goja.Program, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if p, ok := r.programs[scriptPath]; ok {
		return p, nil
	}

	src, err := os.ReadFile(scriptPath)
	if err != nil {
		return nil, fmt.Errorf("reading rule script %s: %w", scriptPath, err)
	}

	program, err := goja.Compile(scriptPath, string(src), false)
	if err != nil {
		return nil, fmt.Errorf("compiling rule script %s: %w", scriptPath, err)
	}

	r.programs[scriptPath] = program
	return program, nil
}

// toJSValue round-trips through JSON so the JS-visible object's property
// names match the documented contract's json tags (e.g. client_profile), not
// input's Go field names.
func toJSValue(input rules.Input) (any, error) {
	b, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	return v, nil
}

func parseOutput(exported any) (rules.Output, error) {
	b, err := json.Marshal(exported)
	if err != nil {
		return rules.Output{}, fmt.Errorf("rule script returned a non-JSON-serializable value: %w", err)
	}
	var out rules.Output
	if err := json.Unmarshal(b, &out); err != nil {
		return rules.Output{}, fmt.Errorf("rule script returned malformed output: %w", err)
	}
	if out.Action != rules.ActionContinue && out.Action != rules.ActionReject {
		return rules.Output{}, fmt.Errorf("rule script returned unknown action %q (want %q or %q)", out.Action, rules.ActionContinue, rules.ActionReject)
	}
	return out, nil
}

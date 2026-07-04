// Package pyrule executes rule scripts written in Python as real OS
// subprocesses, since Go cannot embed a CPython interpreter.
package pyrule

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/shadow-code/shadow-mcp/internal/config"
	"github.com/shadow-code/shadow-mcp/internal/rules"
)

const defaultTimeout = 5 * time.Second

// Runner runs Python rule scripts as `<python> <script>`, writing the Input
// contract as JSON to stdin and reading the Output contract as JSON from
// stdout only (stderr is logged, not parsed). The subprocess does not inherit
// shadow-mcp's own environment - only PATH plus whatever the rule's Env
// allowlist names - so a rule script can't accidentally see downstream
// servers' secrets. Timeouts are enforced via context and kill the process on
// deadline.
type Runner struct {
	// PythonPath overrides the python executable to invoke. If empty,
	// defaults to "python" on Windows and "python3" elsewhere (on Windows,
	// "python3" commonly resolves to a non-functional Microsoft Store stub).
	PythonPath string
}

// NewRunner creates a Python rule runner using the platform-appropriate
// default python executable.
func NewRunner() *Runner {
	return &Runner{}
}

func (r *Runner) pythonPath() string {
	if r.PythonPath != "" {
		return r.PythonPath
	}
	if runtime.GOOS == "windows" {
		return "python"
	}
	return "python3"
}

// Run implements rules.Runner.
func (r *Runner) Run(ctx context.Context, rule *config.Rule, input rules.Input) (rules.Output, error) {
	timeout := rule.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return rules.Output{}, err
	}

	cmd := exec.CommandContext(runCtx, r.pythonPath(), rule.Script)
	cmd.Stdin = bytes.NewReader(inputJSON)
	cmd.Env = buildEnv(rule.Env)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if stderr.Len() > 0 {
		log.Printf("[rule %s] stderr: %s", rule.Name, stderr.String())
	}
	if runErr != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			return rules.Output{}, fmt.Errorf("rule %q timed out after %s", rule.Name, timeout)
		}
		return rules.Output{}, fmt.Errorf("running rule script %s: %w", rule.Script, runErr)
	}

	var output rules.Output
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		return rules.Output{}, fmt.Errorf("rule script %s returned malformed output: %w", rule.Script, err)
	}
	if output.Action != rules.ActionContinue && output.Action != rules.ActionReject {
		return rules.Output{}, fmt.Errorf("rule script %s returned unknown action %q", rule.Script, output.Action)
	}

	return output, nil
}

func buildEnv(allowlist []string) []string {
	env := []string{"PATH=" + os.Getenv("PATH")}
	for _, name := range allowlist {
		if v, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+v)
		}
	}
	return env
}

package rules

import (
	"context"

	"github.com/shadow-code/shadow-mcp/internal/config"
)

// Runner executes one rule's script against a contract Input and returns its
// Output. Each configured rule language (js, python) has its own Runner
// implementation registered on the Engine.
type Runner interface {
	Run(ctx context.Context, rule *config.Rule, input Input) (Output, error)
}

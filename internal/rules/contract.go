// Package rules matches, orders, and chains pre/post tool-call hook scripts.
package rules

// Hook identifies which side of a tool call a rule runs on.
type Hook string

const (
	HookPre  Hook = "pre"
	HookPost Hook = "post"
)

// Action is a rule script's verdict on a tool call.
type Action string

const (
	ActionContinue Action = "continue"
	ActionReject   Action = "reject"
)

// Input is the JSON contract passed to a rule script (identical shape for JS
// and Python runners).
type Input struct {
	Hook            Hook   `json:"hook"`
	RuleName        string `json:"rule_name"`
	ClientProfile   string `json:"client_profile"`
	ServerName      string `json:"server_name"`
	ToolName        string `json:"tool_name"`
	ExposedToolName string `json:"exposed_tool_name"`
	Arguments       any    `json:"arguments,omitempty"`
	Result          any    `json:"result,omitempty"`
}

// Output is the JSON contract a rule script must return.
type Output struct {
	Action    Action `json:"action"`
	Arguments any    `json:"arguments,omitempty"`
	Result    any    `json:"result,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

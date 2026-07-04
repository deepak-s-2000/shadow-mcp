package aggregator

// separator between a downstream server name and a tool's original name in a
// namespaced exposed name, e.g. "github__create_issue". Double underscore keeps
// collision risk with typical single-underscore tool names low.
const separator = "__"

func namespacedName(server, tool string) string {
	return server + separator + tool
}

// candidateName computes a tool's exposed name before collision detection:
// namespaced by default, or bare if the server opted out via `namespace: false`.
func candidateName(server, tool string, namespaced bool) string {
	if namespaced {
		return namespacedName(server, tool)
	}
	return tool
}

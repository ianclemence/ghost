// Package toolcap separates tool definitions (what a tool is) from tool
// executors (where it runs). The planner queries capability advertisements;
// nothing hard-codes "hardware tools only run on the Pi".
package toolcap

// Executor is where a tool can run.
type Executor string

const (
	ExecPhone Executor = "phone"
	ExecPod   Executor = "pod"
	ExecCloud Executor = "cloud"
)

// Definition describes a tool: name, schemas, permissions, placement.
type Definition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema,omitempty"`
	Permissions []string               `json:"permissions,omitempty"`
	Executors   []Executor             `json:"executors"`
	Sensitive   bool                   `json:"sensitive,omitempty"`
}

// Supports reports whether the tool may run on e.
func (d Definition) Supports(e Executor) bool {
	for _, x := range d.Executors {
		if x == e {
			return true
		}
	}
	return false
}

// Advertisement is the protocol-backed capability listing exchanged between
// phone and Pod. Tool availability derived from it is authoritative.
type Advertisement struct {
	DeviceID string       `json:"device_id"`
	Tools    []Definition `json:"tools"`
}

// AvailableOn returns tool names executable on e.
func (a Advertisement) AvailableOn(e Executor) []string {
	var out []string
	for _, t := range a.Tools {
		if t.Supports(e) {
			out = append(out, t.Name)
		}
	}
	return out
}

// PodHardwareTools are the executors that require Pod-owned hardware. This is
// data (derived from executor placement), not a hard-coded branch: a tool
// routes to the Pod because its definition lists only ExecPod.
func PodHardwareTools(tools []Definition) []string {
	var out []string
	for _, t := range tools {
		if len(t.Executors) == 1 && t.Executors[0] == ExecPod {
			out = append(out, t.Name)
		}
	}
	return out
}

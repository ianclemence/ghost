// Package connector is Ghost's unified, portable connector model.
//
// A connector is a capability-scoped package that teaches Ghost how to reach
// an external system. It is deliberately not an "app": it declares what it can
// do (capabilities + risk), how the user authenticates, and where it came from
// — never a UI, a secret, or an authority. The model sees capabilities; the
// runtime resolves a capability to a connector and its vault credential; the
// permission broker decides; runtime evidence proves.
//
// One manifest subsumes every integration mechanism Ghost supports:
//
//	native   an in-process Go capability (first-party)
//	skill    markdown instructions + bounded scripts
//	mcp      any Model Context Protocol server (the universal adapter)
//	openapi  generated from an OpenAPI document
//
// The connector is portable on purpose: a manifest authored for one Ghost
// installs on any other, because nothing in it names a platform, a price, or a
// ranking. Distribution is a directory, never a landlord.
package connector

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ianclemence/ghost/pkg/capability"
	"github.com/ianclemence/ghost/pkg/connectedapp"
)

// SchemaVersion is the manifest schema this build understands.
const SchemaVersion = 1

// FileName is the canonical manifest filename inside a connector directory.
const FileName = "connector.json"

// Kind is the integration mechanism a connector uses.
type Kind string

const (
	KindNative  Kind = "native"  // in-process Go capability (first-party)
	KindSkill   Kind = "skill"   // markdown instructions + bounded scripts
	KindMCP     Kind = "mcp"     // Model Context Protocol server
	KindOpenAPI Kind = "openapi" // generated from an OpenAPI document
)

// Operation binds a capability to a transport operation. For an openapi
// connector it is the HTTP method + path; for an mcp connector it is the
// remote tool name. Native/skill capabilities leave it nil (the runtime
// resolves them through the existing tool registry).
type Operation struct {
	Method string `json:"method,omitempty"`
	Path   string `json:"path,omitempty"`
	Tool   string `json:"tool,omitempty"`
}

// Capability declares one thing a connector can do, with the risk the
// permission broker will enforce. Risk is authored here and never inferred by
// the model.
type Capability struct {
	// ID is the namespaced capability id, e.g. "email.search". Must contain
	// a dot so capabilities never collide with a bare tool name.
	ID string `json:"id"`
	// Title is a short human-readable name.
	Title string `json:"title,omitempty"`
	// Description explains, in product terms, what the capability does.
	Description string `json:"description,omitempty"`
	// Risk is the authority class: read_only, low_risk, consequential,
	// high_impact.
	Risk capability.Risk `json:"risk"`
	// RequiredInput / OptionalInput name the arguments the model must supply.
	RequiredInput []string `json:"required_input,omitempty"`
	OptionalInput []string `json:"optional_input,omitempty"`
	// AllowedTools is the closed execution path once the capability is
	// committed. Empty means the connector's transport decides.
	AllowedTools []string `json:"allowed_tools,omitempty"`
	// NetworkRequired marks capabilities that reach the network.
	NetworkRequired bool `json:"network_required,omitempty"`
	// Deterministic marks local ops that may bypass the model when possible.
	Deterministic bool `json:"deterministic,omitempty"`
	// Operation binds this capability to a transport operation (openapi/mcp).
	Operation *Operation `json:"operation,omitempty"`
}

// Auth declares how the user proves ownership of the external system.
type Auth struct {
	Kind  connectedapp.AuthKind  `json:"kind"`
	Setup connectedapp.SetupKind `json:"setup"`
	// Provider is the vault provider id (defaults to the connector id).
	Provider string `json:"provider,omitempty"`
	// Scopes are the external scopes requested; users may grant read-only first.
	Scopes []string `json:"scopes,omitempty"`
	// Header is the request header that carries the credential (default
	// "Authorization"); Scheme is its prefix (default "Bearer" for the
	// Authorization header, empty otherwise, so X-API-Key carries the raw key).
	Header string `json:"header,omitempty"`
	Scheme string `json:"scheme,omitempty"`
}

// MCPSpec configures a Model Context Protocol connector.
type MCPSpec struct {
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	URL     string            `json:"url,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// OpenAPISpec points at the OpenAPI document a connector was generated from.
type OpenAPISpec struct {
	URL     string `json:"url,omitempty"`
	Path    string `json:"path,omitempty"`
	BaseURL string `json:"base_url,omitempty"`
}

// Provenance records where a connector came from so installs are traceable.
type Provenance struct {
	Source    string `json:"source,omitempty"` // registry | github | clawhub | local
	Repo      string `json:"repo,omitempty"`
	Revision  string `json:"revision,omitempty"`
	Hash      string `json:"hash,omitempty"`
	SignedBy  string `json:"signed_by,omitempty"`
	Signature string `json:"signature,omitempty"` // base64 ed25519 over the content hash
}

// Manifest is the portable connector definition.
type Manifest struct {
	SchemaVersion int          `json:"schema_version"`
	ID            string       `json:"id"`
	DisplayName   string       `json:"display_name"`
	Description   string       `json:"description,omitempty"`
	Kind          Kind         `json:"kind"`
	Version       string       `json:"version"`
	Auth          Auth         `json:"auth"`
	Capabilities  []Capability `json:"capabilities"`
	MCP           *MCPSpec     `json:"mcp,omitempty"`
	OpenAPI       *OpenAPISpec `json:"openapi,omitempty"`
	Provenance    Provenance   `json:"provenance,omitempty"`
}

// ValidationError is one problem found in a manifest.
type ValidationError struct {
	Field string
	Msg   string
}

func (e ValidationError) Error() string {
	if e.Field == "" {
		return e.Msg
	}
	return e.Field + ": " + e.Msg
}

// FormatErrors renders validation errors one per line.
func FormatErrors(errs []ValidationError) string {
	if len(errs) == 0 {
		return ""
	}
	parts := make([]string, len(errs))
	for i, e := range errs {
		parts[i] = e.Error()
	}
	return strings.Join(parts, "\n")
}

var slugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// mutatingVerbs are the write-shaped verbs that must never appear in a
// capability declared read_only. This is the manifest-level half of the
// capability-risk audit; the runtime broker is the other half.
var mutatingVerbs = []string{
	"create", "update", "delete", "remove", "send", "write", "post", "put",
	"patch", "submit", "publish", "upload", "push", "set", "add", "edit",
	"cancel", "revoke", "grant", "transfer", "pay", "purchase", "order", "book",
}

// looksMutating reports whether a tool name contains a write-shaped verb.
func looksMutating(tool string) bool {
	t := strings.ToLower(tool)
	for _, v := range mutatingVerbs {
		if strings.Contains(t, v) {
			return true
		}
	}
	return false
}

// Validate returns every problem with the manifest. An empty slice is valid.
// It never mutates the manifest and never touches the network.
func (m *Manifest) Validate() []ValidationError {
	if m == nil {
		return []ValidationError{{Msg: "nil manifest"}}
	}
	var errs []ValidationError
	add := func(field, msg string) { errs = append(errs, ValidationError{Field: field, Msg: msg}) }

	if m.SchemaVersion != SchemaVersion {
		add("schema_version", fmt.Sprintf("unsupported schema version %d (want %d)", m.SchemaVersion, SchemaVersion))
	}
	if !slugRE.MatchString(m.ID) || len(m.ID) > 64 {
		add("id", "must be a lowercase slug matching ^[a-z0-9][a-z0-9._-]*$ (max 64 chars)")
	}
	if strings.TrimSpace(m.DisplayName) == "" {
		add("display_name", "required")
	}
	switch m.Kind {
	case KindNative, KindSkill, KindMCP, KindOpenAPI:
	default:
		add("kind", fmt.Sprintf("unknown kind %q (want native|skill|mcp|openapi)", m.Kind))
	}
	if strings.TrimSpace(m.Version) == "" {
		add("version", "required")
	}

	errs = append(errs, m.validateAuth()...)
	errs = append(errs, m.validateCapabilities()...)
	errs = append(errs, m.validateTransport()...)

	if m.Provenance.SignedBy != "" && m.Provenance.Hash == "" {
		add("provenance.hash", "a signed connector must carry a content hash")
	}
	if m.Provenance.Signature != "" && m.Provenance.SignedBy == "" {
		add("provenance.signed_by", "a signature without a signer is meaningless")
	}
	return errs
}

func (m *Manifest) validateAuth() []ValidationError {
	var errs []ValidationError
	add := func(field, msg string) { errs = append(errs, ValidationError{Field: field, Msg: msg}) }

	switch m.Auth.Kind {
	case connectedapp.AuthOAuth:
		if m.Auth.Setup != "" && m.Auth.Setup != connectedapp.SetupConsoleOAuth {
			add("auth.setup", "oauth connectors must use console_oauth setup (never a pasted secret)")
		}
	case connectedapp.AuthAPIKey, connectedapp.AuthToken:
		switch m.Auth.Setup {
		case "", connectedapp.SetupPasteKey, connectedapp.SetupPastePair:
		default:
			add("auth.setup", fmt.Sprintf("setup %q is not valid for %s auth", m.Auth.Setup, m.Auth.Kind))
		}
	case "":
		// Keyless connector (e.g. a public fallback provider). Allowed.
	default:
		add("auth.kind", fmt.Sprintf("unknown auth kind %q (want oauth|api_key|token or empty)", m.Auth.Kind))
	}
	return errs
}

func (m *Manifest) validateCapabilities() []ValidationError {
	var errs []ValidationError
	add := func(field, msg string) { errs = append(errs, ValidationError{Field: field, Msg: msg}) }

	if len(m.Capabilities) == 0 {
		add("capabilities", "a connector must declare at least one capability")
		return errs
	}
	seen := map[string]bool{}
	for i, c := range m.Capabilities {
		f := fmt.Sprintf("capabilities[%d]", i)
		if !slugRE.MatchString(c.ID) || !strings.Contains(c.ID, ".") {
			add(f+".id", "must be a namespaced slug containing a dot, e.g. \"email.search\"")
		}
		if seen[c.ID] {
			add(f+".id", fmt.Sprintf("duplicate capability id %q", c.ID))
		}
		seen[c.ID] = true

		switch c.Risk {
		case capability.RiskReadOnly, capability.RiskLow, capability.RiskConsequential, capability.RiskHighImpact:
		default:
			add(f+".risk", fmt.Sprintf("unknown risk %q (want read_only|low_risk|consequential|high_impact)", c.Risk))
		}
		// The audit: a read_only capability may not name a mutating tool.
		if c.Risk == capability.RiskReadOnly {
			for _, t := range c.AllowedTools {
				if looksMutating(t) {
					add(f+".allowed_tools", fmt.Sprintf("read_only capability names a mutating tool %q", t))
				}
			}
		}
		// Executable transports must bind each capability to an operation.
		switch m.Kind {
		case KindOpenAPI:
			if c.Operation == nil || strings.TrimSpace(c.Operation.Method) == "" || strings.TrimSpace(c.Operation.Path) == "" {
				add(f+".operation", "an openapi capability needs an operation with method and path")
			}
		case KindMCP:
			if c.Operation == nil || strings.TrimSpace(c.Operation.Tool) == "" {
				add(f+".operation", "an mcp capability needs an operation naming the remote tool")
			}
		}
	}
	return errs
}

func (m *Manifest) validateTransport() []ValidationError {
	var errs []ValidationError
	add := func(field, msg string) { errs = append(errs, ValidationError{Field: field, Msg: msg}) }

	switch m.Kind {
	case KindMCP:
		if m.MCP == nil || (strings.TrimSpace(m.MCP.Command) == "" && strings.TrimSpace(m.MCP.URL) == "") {
			add("mcp", "an mcp connector needs a command or a url")
		}
		if m.OpenAPI != nil {
			add("openapi", "must be empty for an mcp connector")
		}
	case KindOpenAPI:
		// Execution needs the base URL; url/path are optional provenance of
		// the source document.
		if m.OpenAPI == nil || strings.TrimSpace(m.OpenAPI.BaseURL) == "" {
			add("openapi", "an openapi connector needs a base_url to execute against")
		}
		if m.MCP != nil {
			add("mcp", "must be empty for an openapi connector")
		}
	default:
		if m.MCP != nil || m.OpenAPI != nil {
			add("kind", "native/skill connectors must not set mcp or openapi specs")
		}
	}
	return errs
}

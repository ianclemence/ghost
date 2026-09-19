package connector

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ianclemence/ghost/pkg/capability"
	"github.com/ianclemence/ghost/pkg/connectedapp"
)

// openAPIDoc is the subset of an OpenAPI 3 document the generator reads. It is
// intentionally small: the generator produces a reviewable draft, not a
// finished connector, so it only needs titles, servers, and operations.
type openAPIDoc struct {
	Info struct {
		Title       string `json:"title"`
		Version     string `json:"version"`
		Description string `json:"description"`
	} `json:"info"`
	Servers []struct {
		URL string `json:"url"`
	} `json:"servers"`
	Paths map[string]map[string]openAPIOp `json:"paths"`
}

type openAPIOp struct {
	OperationID string `json:"operationId"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
}

// OpenAPIOptions tunes draft generation.
type OpenAPIOptions struct {
	// ID overrides the connector id (default: slug of the API title).
	ID string
	// Version overrides the connector version (default: the API version).
	Version string
	// Path is the local file the document was read from (recorded in the
	// openapi spec block for reproducibility).
	Path string
	// URL is the remote document location, when known.
	URL string
	// Auth overrides the default (api_key + paste_key). Set to a zero Auth{}
	// for a keyless API.
	Auth *Auth
}

// FromOpenAPI builds a draft connector manifest from an OpenAPI document. The
// draft is deliberately conservative: every operation becomes a capability;
// GET/HEAD/OPTIONS are read_only, mutating methods are consequential. The
// author must review and tighten it (required inputs, scopes, auth) before
// publishing.
func FromOpenAPI(doc []byte, opts OpenAPIOptions) (*Manifest, error) {
	var d openAPIDoc
	if err := json.Unmarshal(doc, &d); err != nil {
		return nil, fmt.Errorf("parse openapi: %w", err)
	}
	title := strings.TrimSpace(d.Info.Title)
	if title == "" {
		title = "connector"
	}
	id := opts.ID
	if id == "" {
		id = Slugify(title)
	}
	if id == "" {
		return nil, fmt.Errorf("could not derive a connector id from the document")
	}
	version := opts.Version
	if version == "" {
		version = strings.TrimSpace(d.Info.Version)
	}
	if version == "" {
		version = "0.1.0"
	}

	baseURL := ""
	if len(d.Servers) > 0 {
		baseURL = strings.TrimSpace(d.Servers[0].URL)
	}

	auth := Auth{Kind: connectedapp.AuthAPIKey, Setup: connectedapp.SetupPasteKey}
	if opts.Auth != nil {
		auth = *opts.Auth
	}

	paths := make([]string, 0, len(d.Paths))
	for p := range d.Paths {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	seen := map[string]bool{}
	var caps []Capability
	for _, p := range paths {
		methods := make([]string, 0, len(d.Paths[p]))
		for m := range d.Paths[p] {
			methods = append(methods, strings.ToLower(m))
		}
		sort.Strings(methods)
		for _, method := range methods {
			switch method {
			case "get", "head", "options", "post", "put", "patch", "delete":
			default:
				continue
			}
			op := d.Paths[p][method]
			opSlug := Slugify(op.OperationID)
			if opSlug == "" {
				opSlug = Slugify(method + " " + p)
			}
			capID := id + "." + opSlug
			if seen[capID] {
				capID = fmt.Sprintf("%s.%s-%s", id, opSlug, method)
			}
			seen[capID] = true

			risk := capability.RiskReadOnly
			switch method {
			case "post", "put", "patch", "delete":
				risk = capability.RiskConsequential
			}
			desc := strings.TrimSpace(op.Summary)
			if desc == "" {
				desc = strings.TrimSpace(op.Description)
			}
			caps = append(caps, Capability{
				ID:              capID,
				Title:           strings.TrimSpace(op.Summary),
				Description:     desc,
				Risk:            risk,
				NetworkRequired: true,
				Operation:       &Operation{Method: strings.ToUpper(method), Path: p},
			})
		}
	}
	if len(caps) == 0 {
		return nil, fmt.Errorf("document declares no usable operations")
	}

	return &Manifest{
		SchemaVersion: SchemaVersion,
		ID:            id,
		DisplayName:   title,
		Description:   strings.TrimSpace(d.Info.Description),
		Kind:          KindOpenAPI,
		Version:       version,
		Auth:          auth,
		Capabilities:  caps,
		OpenAPI: &OpenAPISpec{
			URL:     strings.TrimSpace(opts.URL),
			Path:    strings.TrimSpace(opts.Path),
			BaseURL: baseURL,
		},
		Provenance: Provenance{Source: "openapi"},
	}, nil
}

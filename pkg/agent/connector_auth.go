package agent

import (
	"strings"

	"github.com/ianclemence/ghost/pkg/connector"
	"github.com/ianclemence/ghost/pkg/credentials"
)

// connectorAuthHeaders supplies the auth header for an installed connector from
// the credential store (.secrets.json ProviderAPIKeys, then <ID>_API_KEY env).
// The manifest chooses the header and scheme: default is
// `Authorization: Bearer <key>`; a connector using `X-API-Key` gets the raw
// key. It returns nil (no auth) when the connector is keyless or unconfigured,
// so a keyless connector still works and an unconfigured one fails honestly at
// call time rather than sending a bad credential.
func connectorAuthHeaders(m *connector.Manifest) (map[string]string, error) {
	if m == nil {
		return nil, nil
	}
	id := strings.TrimSpace(m.Auth.Provider)
	if id == "" {
		id = strings.TrimSpace(m.ID)
	}
	if id == "" {
		return nil, nil
	}
	key := credentials.ProviderKey(id)
	if key == "" {
		return nil, nil
	}
	header := strings.TrimSpace(m.Auth.Header)
	if header == "" {
		header = "Authorization"
	}
	scheme := strings.TrimSpace(m.Auth.Scheme)
	if scheme == "" && header == "Authorization" {
		scheme = "Bearer"
	}
	value := key
	if scheme != "" {
		value = scheme + " " + key
	}
	return map[string]string{header: value}, nil
}

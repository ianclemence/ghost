package agent

import (
	"strings"

	"github.com/ianclemence/ghost/pkg/credentials"
)

// connectorAuthHeaders supplies Bearer auth for an installed connector from the
// credential store (.secrets.json ProviderAPIKeys, then <ID>_API_KEY env). It
// returns nil (no auth) when the connector is keyless or unconfigured, so a
// keyless connector still works and an unconfigured one fails honestly at call
// time rather than sending a bad credential.
func connectorAuthHeaders(connectorID, provider string) (map[string]string, error) {
	id := strings.TrimSpace(provider)
	if id == "" {
		id = strings.TrimSpace(connectorID)
	}
	if id == "" {
		return nil, nil
	}
	key := credentials.ProviderKey(id)
	if key == "" {
		return nil, nil
	}
	return map[string]string{"Authorization": "Bearer " + key}, nil
}

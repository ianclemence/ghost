package agent

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/ianclemence/ghost/pkg/permissions"
)

// An approval has to run the action the owner was shown, with its parameters.
// The pause record is a map of strings, so a value that is not a string (a
// boolean, a number, a list) travels as JSON behind a marker. Anything that
// cannot be kept faithfully (a secret-shaped argument, a value too large) is
// named in contDroppedKey, and a resume that would run without it is refused:
// running a smaller action than the one approved, with recursive or dry_run
// quietly gone, is not an approval.
const (
	contJSONPrefix = "\x00json:"
	contDroppedKey = "_dropped"
	maxContValue   = 16 << 10
	maxContTotal   = 64 << 10
)

func continuationOf(args map[string]interface{}) map[string]string {
	out := map[string]string{}
	var dropped []string
	total := 0
	for k, v := range args {
		if permissions.SecretShapedName(k) {
			dropped = append(dropped, k)
			continue
		}
		var enc string
		if s, ok := v.(string); ok {
			enc = s
		} else {
			b, err := json.Marshal(v)
			if err != nil {
				dropped = append(dropped, k)
				continue
			}
			enc = contJSONPrefix + string(b)
		}
		if len(enc) > maxContValue || total+len(enc) > maxContTotal {
			dropped = append(dropped, k)
			continue
		}
		total += len(enc)
		out[k] = enc
	}
	if len(dropped) > 0 {
		sort.Strings(dropped)
		out[contDroppedKey] = strings.Join(dropped, ",")
	}
	return out
}

// argsFromContinuation rebuilds the arguments an approval was asked about, with
// their original types, and the names of any that could not be kept.
func argsFromContinuation(cont map[string]string) (map[string]interface{}, []string) {
	args := make(map[string]interface{}, len(cont))
	var dropped []string
	for k, v := range cont {
		if k == contDroppedKey {
			dropped = strings.Split(v, ",")
			continue
		}
		if strings.HasPrefix(v, contJSONPrefix) {
			var decoded interface{}
			if err := json.Unmarshal([]byte(strings.TrimPrefix(v, contJSONPrefix)), &decoded); err == nil {
				args[k] = decoded
				continue
			}
		}
		args[k] = v
	}
	return args, dropped
}

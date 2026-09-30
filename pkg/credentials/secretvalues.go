package credentials

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// minScrubLen is the shortest secret worth scrubbing by value. Shorter
// strings collide with ordinary words and would corrupt honest output.
const minScrubLen = 6

var (
	secretCacheMu   sync.Mutex
	secretCacheAt   time.Time
	secretCacheDir  string
	secretCacheVals []string
)

// SecretValues returns every raw secret currently held by the vault — API
// keys, channel tokens, website-login passwords — longest first. It exists
// for exactly one caller: the tool-result boundary, which removes these
// values from anything headed to the model or the transcript. Nothing else
// should call it.
func SecretValues() []string {
	dir := configDirForWrites()
	secretCacheMu.Lock()
	defer secretCacheMu.Unlock()
	if dir == secretCacheDir && time.Since(secretCacheAt) < 3*time.Second {
		return secretCacheVals
	}
	vals := collectSecretValues(filepath.Join(dir, ".secrets.json"))
	secretCacheDir, secretCacheAt, secretCacheVals = dir, time.Now(), vals
	return vals
}

func collectSecretValues(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var root interface{}
	if json.Unmarshal(data, &root) != nil {
		return nil
	}
	seen := map[string]bool{}
	var walk func(v interface{})
	add := func(s string) {
		s = strings.TrimSpace(s)
		if len(s) >= minScrubLen && s != "oauth:connected" {
			seen[s] = true
		}
	}
	walk = func(v interface{}) {
		switch t := v.(type) {
		case map[string]interface{}:
			for _, x := range t {
				walk(x)
			}
		case []interface{}:
			for _, x := range t {
				walk(x)
			}
		case string:
			add(t)
			// Website logins are sealed as a JSON blob; the password inside
			// is the secret, the blob is just its carrier.
			if strings.HasPrefix(strings.TrimSpace(t), "{") {
				var l WebLogin
				if json.Unmarshal([]byte(t), &l) == nil {
					add(l.Password)
				}
			}
		}
	}
	walk(root)
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

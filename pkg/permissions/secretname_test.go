package permissions

import "testing"

func TestSecretShapedNameReadsWordsNotSubstrings(t *testing.T) {
	secret := []string{"api_key", "apiKey", "API-KEY", "key", "token", "access_token", "client_secret", "password", "Authorization_Bearer", "credentials", "passphrase", "cookie"}
	plain := []string{"keyword", "keywords", "keyboard", "monkey", "tokenizer_name", "timeout_seconds", "recursive", "path", "to", "content", "surname", "turkey"}
	for _, n := range secret {
		if !SecretShapedName(n) {
			t.Errorf("%q holds a secret and must be treated as one", n)
		}
	}
	for _, n := range plain {
		if SecretShapedName(n) {
			t.Errorf("%q is an ordinary argument and must survive an approval", n)
		}
	}
}

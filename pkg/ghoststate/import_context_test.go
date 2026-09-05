package ghoststate

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/personalcontext"
)

func TestTagUntaggedEntries(t *testing.T) {
	line := func(scopes string) string {
		if scopes == "" {
			return `{"id":"a","kind":"fact","subject":"user","predicate":"likes","value":"\"tea\"","status":"current"}` + "\n"
		}
		return `{"id":"b","kind":"fact","subject":"user","predicate":"project","value":"\"x\"","status":"current","scopes":["context:home"]}` + "\n"
	}
	out, err := tagUntaggedEntries([]byte(line("")+line("x")), "work")
	if err != nil {
		t.Fatal(err)
	}
	var recs []map[string]interface{}
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		var r map[string]interface{}
		if err := json.Unmarshal([]byte(l), &r); err != nil {
			t.Fatal(err)
		}
		recs = append(recs, r)
	}
	if len(recs) != 2 {
		t.Fatal("must preserve both records")
	}
	scopesOf := func(r map[string]interface{}) []string {
		var s []string
		for _, v := range r["scopes"].([]interface{}) {
			s = append(s, v.(string))
		}
		return s
	}
	if got := scopesOf(recs[0]); len(got) != 1 || got[0] != "context:work" {
		t.Fatalf("untagged must gain scope: %v", got)
	}
	if got := scopesOf(recs[1]); len(got) != 1 || got[0] != "context:home" {
		t.Fatalf("tagged must keep scope: %v", got)
	}
	// Result still validates as an entry log.
	if err := personalcontext.ValidateEntries(out); err != nil {
		t.Fatalf("tagged output must validate: %v", err)
	}
}

func TestTagInvalidContext(t *testing.T) {
	for _, bad := range []string{"../evil", "Work Space", "", "a_very_long_" + strings.Repeat("x", 100)} {
		if _, err := tagUntaggedEntries([]byte("{}\n"), bad); err == nil {
			t.Fatalf("%q must fail closed", bad)
		}
	}
}

func TestTagMalformedLine(t *testing.T) {
	if _, err := tagUntaggedEntries([]byte("not json\n"), "work"); err == nil {
		t.Fatal("malformed record must fail, not half-tag")
	}
}

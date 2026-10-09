package credentials

import (
	"strings"
	"testing"
)

func TestDatabasesAreSealedAndScrubbed(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GHOST_CONFIG_DIR", dir)
	if err := SaveDatabase(Database{Name: "Shop Sales", URL: "postgres://reader:s3cret-pass@db.example.com:5432/shop"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveDatabase(Database{Name: "x", URL: "mysql://a@b/c"}); err == nil {
		t.Fatal("only postgres")
	}
	list := ListDatabases()
	if len(list) != 1 || list[0].Name != "shop-sales" || list[0].Host != "db.example.com" || list[0].DB != "shop" || list[0].User != "reader" {
		t.Fatalf("list: %+v", list)
	}
	d, ok := DatabaseFor("Shop Sales")
	if !ok || !strings.Contains(d.URL, "s3cret-pass") {
		t.Fatal("for")
	}
	found := false
	for _, v := range collectSecretValues(dir + "/.secrets.json") {
		if v == "s3cret-pass" {
			found = true
		}
	}
	if !found {
		t.Fatal("the password is scrubbed from what the model sees")
	}
	if err := DeleteDatabase("shop-sales"); err != nil {
		t.Fatal(err)
	}
	if len(ListDatabases()) != 0 {
		t.Fatal("deleted")
	}
}

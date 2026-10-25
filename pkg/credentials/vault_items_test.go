package credentials

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func vaultDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GHOST_CONFIG_DIR", dir)
	return dir
}

func TestCardBrandAndLuhn(t *testing.T) {
	cases := []struct {
		number, brand string
		luhn          bool
	}{
		{"4242 4242 4242 4242", "visa", true},
		{"4111111111111111", "visa", true},
		{"5555 5555 5555 4444", "mastercard", true},
		{"2223 0031 2200 3222", "mastercard", true},
		{"3782 822463 10005", "amex", true},
		{"6011 1111 1111 1117", "discover", true},
		{"3056 9309 0259 04", "diners", true},
		{"3530 1113 3330 0000", "jcb", true},
		{"6200 0000 0000 0005", "unionpay", true},
		{"5061 0000 0000 0000 007", "verve", true},
		{"4242 4242 4242 4241", "visa", false},
		{"1234", "", false},
	}
	for _, c := range cases {
		if got := CardBrand(c.number); got != c.brand {
			t.Errorf("CardBrand(%q) = %q, want %q", c.number, got, c.brand)
		}
		if got := LuhnValid(digitsOnly.ReplaceAllString(c.number, "")); got != c.luhn {
			t.Errorf("LuhnValid(%q) = %v, want %v", c.number, got, c.luhn)
		}
	}
}

func TestSiteMatchingIsStrictEnoughToTrust(t *testing.T) {
	same := [][2]string{
		{"ads.google.com", "accounts.google.com"},
		{"google.com", "www.google.com"},
		{"https://shop.example.co.uk/login", "example.co.uk"},
		{"app.example.co.ke", "example.co.ke"},
	}
	for _, c := range same {
		if !SameSite(c[0], c[1]) {
			t.Errorf("%q and %q are one site", c[0], c[1])
		}
	}
	other := [][2]string{
		{"google.com.evil.example", "google.com"},
		{"evil-google.com", "google.com"},
		{"google.com", "google.co"},
		{"example.co.uk", "other.co.uk"},
		{"", ""},
		{"a.co.ke", "b.co.ke"},
		{"good.github.io", "evil.github.io"},
		{"my-app.vercel.app", "your-app.vercel.app"},
	}
	for _, c := range other {
		if SameSite(c[0], c[1]) {
			t.Errorf("%q and %q are different sites", c[0], c[1])
		}
	}
}

// What the owner's phone is shown never carries a secret, and a login saved in
// the Vault is the very thing the browser reads.
func TestLoginInTheVaultIsTheOneTheBrowserUses(t *testing.T) {
	vaultDir(t)
	v, err := SaveItem(Item{Kind: KindLogin, URL: "https://ads.google.com", Username: "ian@example.com", Password: "correct horse battery"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Host != "ads.google.com" || v.Username != "ian@example.com" || !v.HasPassword {
		t.Fatalf("view: %+v", v)
	}
	if b, _ := json.Marshal(v); bytes.Contains(b, []byte("correct horse")) {
		t.Fatalf("the owner's view carries the password: %s", b)
	}
	login, ok := WebLoginFor("ads.google.com")
	if !ok || login.Password != "correct horse battery" || login.Username != "ian@example.com" {
		t.Fatalf("the browser cannot use a Vault login: %+v ok=%v", login, ok)
	}
	// Editing the username must not ask for the password again.
	if _, err := SaveItem(Item{ID: "ads.google.com", Kind: KindLogin, Host: "ads.google.com", URL: "https://ads.google.com", Username: "work@example.com"}); err != nil {
		t.Fatal(err)
	}
	login, _ = WebLoginFor("ads.google.com")
	if login.Username != "work@example.com" || login.Password != "correct horse battery" {
		t.Fatalf("an edit lost or changed the password: %+v", login)
	}
	if err := DeleteItem(KindLogin, "ads.google.com"); err != nil {
		t.Fatal(err)
	}
	if _, ok := WebLoginFor("ads.google.com"); ok {
		t.Fatal("a deleted login is still usable")
	}
}

func TestACardIsSealedAndGhostNeverSeesItsNumber(t *testing.T) {
	dir := vaultDir(t)
	v, err := SaveItem(Item{Kind: KindCard, Title: "Personal Visa", Number: "4242 4242 4242 4242", ExpMonth: 8, ExpYear: 28, CVC: "123", Holder: "Ian Clemence"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Brand != "visa" || v.Last4 != "4242" || v.ExpYear != 2028 || !v.HasCVC {
		t.Fatalf("view: %+v", v)
	}
	owner, _ := json.Marshal(ListItems(KindCard))
	model, _ := json.Marshal(ModelItems(KindCard))
	for name, b := range map[string][]byte{"owner view": owner, "model view": model} {
		for _, secret := range []string{"4242424242424242", "4242 4242 4242 4242", "123\""} {
			if bytes.Contains(b, []byte(secret)) {
				t.Fatalf("%s carries %q: %s", name, secret, b)
			}
		}
	}
	if !strings.Contains(string(model), "ending 4242") {
		t.Fatalf("Ghost should know which card it is: %s", model)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".secrets.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("4242")) || bytes.Contains(raw, []byte("Ian Clemence")) {
		t.Fatal("card details are on disk in the clear")
	}
	// The scrubber at the tool-result boundary knows every spelling of it.
	vals := strings.Join(SecretValues(), "|")
	for _, want := range []string{"4242424242424242", "4242 4242 4242 4242", "4242-4242-4242-4242"} {
		if !strings.Contains(vals, want) {
			t.Errorf("the scrubber does not know %q", want)
		}
	}
}

func TestCardValidation(t *testing.T) {
	vaultDir(t)
	bad := []Item{
		{Kind: KindCard, Number: "4242 4242 4242 4241", ExpMonth: 8, ExpYear: 28},
		{Kind: KindCard, Number: "4242 4242 4242 4242", ExpMonth: 13, ExpYear: 28},
		{Kind: KindCard, Number: "4242 4242 4242 4242", ExpMonth: 8, ExpYear: 28, CVC: "12"},
		{Kind: KindCard, ExpMonth: 8, ExpYear: 28},
	}
	for i, it := range bad {
		if _, err := SaveItem(it); err == nil {
			t.Errorf("case %d should be refused", i)
		}
	}
	v, err := SaveItem(Item{Kind: KindCard, Number: "4000056655665556", ExpMonth: 1, ExpYear: 20})
	if err != nil {
		t.Fatal(err)
	}
	if !v.Expired {
		t.Fatalf("an old card must say it has expired: %+v", v)
	}
}

func TestContactsAreReadableByGhost(t *testing.T) {
	vaultDir(t)
	if _, err := SaveItem(Item{Kind: KindContact, Name: "Amina Hassan", Email: "amina@example.com", Phone: "+254 700 000 000", City: "Nairobi", Country: "Kenya"}); err != nil {
		t.Fatal(err)
	}
	line := ModelItems(KindContact)[0].Line
	for _, want := range []string{"Amina Hassan", "amina@example.com", "+254 700 000 000", "Nairobi"} {
		if !strings.Contains(line, want) {
			t.Errorf("Ghost should be able to read %q: %q", want, line)
		}
	}
	for _, bad := range []Item{{Kind: KindContact}, {Kind: KindContact, Name: "x", Email: "not an email"}, {Kind: KindContact, Name: "x", Phone: "call me"}} {
		if _, err := SaveItem(bad); err == nil {
			t.Errorf("%+v should be refused", bad)
		}
	}
}

// A key reaches a command only when the owner allowed it AND the command
// names it; otherwise it is not there.
func TestAKeyReachesOnlyACommandThatNamesIt(t *testing.T) {
	vaultDir(t)
	if _, err := SaveItem(Item{Kind: KindKey, Title: "Stripe", Service: "Stripe", Value: "sk_live_abcdef123456", EnvName: "STRIPE_API_KEY"}); err != nil {
		t.Fatal(err)
	}
	if len(CommandKeys()) != 0 {
		t.Fatal("a key the owner has not allowed must never be offered to commands")
	}
	if _, err := SaveItem(Item{Kind: KindKey, Title: "Cloud", Service: "Cloud", Value: "ck_live_zyxwvu987654", EnvName: "CLOUD_TOKEN", UseInCommands: true}); err != nil {
		t.Fatal(err)
	}
	if got := KeysNamedIn("curl -H \"Authorization: $CLOUD_TOKEN\" https://api.example.com"); got["CLOUD_TOKEN"] != "ck_live_zyxwvu987654" {
		t.Fatalf("an allowed key named in the command should arrive: %v", got)
	}
	if got := KeysNamedIn("env"); len(got) != 0 {
		t.Fatalf("a command that does not name a key must not receive it: %v", got)
	}
	if got := KeysNamedIn("echo $STRIPE_API_KEY"); len(got) != 0 {
		t.Fatalf("a key not allowed for commands must not arrive even when named: %v", got)
	}
	if _, err := SaveItem(Item{Kind: KindKey, Title: "bad", Value: "xxxxxxxx", UseInCommands: true}); err == nil {
		t.Fatal("allowing commands without a variable name must be refused")
	}
	if _, err := SaveItem(Item{Kind: KindKey, Title: "bad", Value: "xxxxxxxx", EnvName: "lower case"}); err == nil {
		t.Fatal("a bad variable name must be refused")
	}
	view, _ := json.Marshal(ListItems(KindKey))
	if bytes.Contains(view, []byte("sk_live_abcdef123456")) || bytes.Contains(view, []byte("ck_live_zyxwvu987654")) {
		t.Fatalf("the owner's view carries a key: %s", view)
	}
	model, _ := json.Marshal(ModelItems(KindKey))
	if bytes.Contains(model, []byte("sk_live")) || bytes.Contains(model, []byte("ck_live")) {
		t.Fatalf("Ghost can read a key: %s", model)
	}
}

func TestAnUnsavedSignInIsHeldBrieflyAndScrubbed(t *testing.T) {
	vaultDir(t)
	if err := PutEphemeralWebLogin(WebLogin{URL: "https://ads.google.com", Username: "ian", Password: "one-task-password"}); err != nil {
		t.Fatal(err)
	}
	l, ok := WebLoginFor("ads.google.com")
	if !ok || l.Password != "one-task-password" {
		t.Fatalf("the browser should find a held sign-in: %+v %v", l, ok)
	}
	if !strings.Contains(strings.Join(SecretValues(), "|"), "one-task-password") {
		t.Fatal("a held password must be scrubbed from anything the model sees")
	}
	if len(ListItems("")) != 0 {
		t.Fatal("a held sign-in is not saved and must not be listed")
	}
	ForgetEphemeralWebLogin("ads.google.com")
	if _, ok := WebLoginFor("ads.google.com"); ok {
		t.Fatal("a forgotten sign-in is still usable")
	}
	if err := PutEphemeralWebLogin(WebLogin{URL: "https://x.example", Username: "u"}); err == nil {
		t.Fatal("a sign-in without a password must be refused")
	}
}

func TestListOrdersByKindThenNewest(t *testing.T) {
	vaultDir(t)
	SaveItem(Item{Kind: KindKey, Title: "k1", Value: "value-one-111"})
	SaveItem(Item{Kind: KindContact, Name: "Zed"})
	SaveItem(Item{Kind: KindLogin, URL: "https://a.example", Username: "u", Password: "p"})
	time.Sleep(5 * time.Millisecond)
	SaveItem(Item{Kind: KindContact, Name: "Amy"})
	var kinds []string
	for _, v := range ListItems("") {
		kinds = append(kinds, string(v.Kind)+":"+v.Title)
	}
	want := []string{"login:a.example", "contact:Amy", "contact:Zed", "key:k1"}
	if strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", kinds, want)
	}
}

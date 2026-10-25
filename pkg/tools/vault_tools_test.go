package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/cards"
	"github.com/ianclemence/ghost/pkg/credentials"
)

func vaultFixture(t *testing.T) {
	t.Helper()
	t.Setenv("GHOST_CONFIG_DIR", t.TempDir())
}

// "Ghost never sees a saved card's number or security code": the one tool that
// reads the Vault for the model, over every kind.
func TestGhostReadsTheVaultButNeverASecret(t *testing.T) {
	vaultFixture(t)
	for _, it := range []credentials.Item{
		{Kind: credentials.KindLogin, URL: "https://ads.google.com", Username: "ian@example.com", Password: "never-show-this-pw"},
		{Kind: credentials.KindCard, Title: "Personal Visa", Number: "4242 4242 4242 4242", ExpMonth: 8, ExpYear: 28, CVC: "123", Holder: "Ian C"},
		{Kind: credentials.KindContact, Name: "Amina Hassan", Email: "amina@example.com", Phone: "+254700000000"},
		{Kind: credentials.KindKey, Title: "Stripe", Service: "Stripe", Value: "sk_live_never_show_key"},
	} {
		if _, err := credentials.SaveItem(it); err != nil {
			t.Fatal(err)
		}
	}
	res := NewVaultItemsTool().Execute(context.Background(), nil)
	if res.IsError {
		t.Fatal(res.ForLLM)
	}
	for _, secret := range []string{"never-show-this-pw", "4242 4242 4242 4242", "4242424242424242", "sk_live_never_show_key", "123", "Ian C"} {
		if strings.Contains(res.ForLLM, secret) {
			t.Fatalf("Ghost was shown %q:\n%s", secret, res.ForLLM)
		}
	}
	for _, want := range []string{"ads.google.com · ian@example.com", "ending 4242", "Amina Hassan", "amina@example.com", "Stripe"} {
		if !strings.Contains(res.ForLLM, want) {
			t.Errorf("Ghost should be able to read %q:\n%s", want, res.ForLLM)
		}
	}
	if one := NewVaultItemsTool().Execute(context.Background(), map[string]interface{}{"kind": "contact"}); strings.Contains(one.ForLLM, "ads.google.com") {
		t.Fatalf("kind should narrow the answer: %s", one.ForLLM)
	}
	if bad := NewVaultItemsTool().Execute(context.Background(), map[string]interface{}{"kind": "wallet"}); !bad.IsError {
		t.Fatal("an unknown kind should be refused")
	}
}

func TestAnEmptyVaultSaysSo(t *testing.T) {
	vaultFixture(t)
	if res := NewVaultItemsTool().Execute(context.Background(), nil); !strings.Contains(res.ForLLM, "empty") {
		t.Fatalf("%s", res.ForLLM)
	}
}

// A key the owner allowed commands to use is useless if Ghost does not know how
// to hand it over: the command must name the variable, and the value reaches
// only that command.
func TestACommandKeyTellsGhostHowToUseIt(t *testing.T) {
	vaultFixture(t)
	if _, err := credentials.SaveItem(credentials.Item{
		Kind: credentials.KindKey, Title: "DeepSeek", Service: "DeepSeek",
		Value: "sk_live_never_show_key", EnvName: "DEEPSEEK_API_KEY", UseInCommands: true,
	}); err != nil {
		t.Fatal(err)
	}
	res := NewVaultItemsTool().Execute(context.Background(), nil)
	if res.IsError {
		t.Fatal(res.ForLLM)
	}
	if strings.Contains(res.ForLLM, "sk_live_never_show_key") {
		t.Fatalf("the key value reached the model: %s", res.ForLLM)
	}
	for _, want := range []string{"commands can read it as $DEEPSEEK_API_KEY", "DEEPSEEK_API_KEY=$DEEPSEEK_API_KEY", "never print it"} {
		if !strings.Contains(res.ForLLM, want) {
			t.Errorf("Ghost should be told %q:\n%s", want, res.ForLLM)
		}
	}
}

func credentialsTool(published *[]cards.Card) *RequestCredentialsTool {
	tool := NewRequestCredentialsTool()
	tool.SetContext("mobile", "default")
	tool.SetPublisher(func(channel, chatID, sessionID string, c cards.Card) {
		*published = append(*published, c)
		cards.DefaultStore.Add(channel, c)
	})
	return tool
}

// "Pause my Google Ads campaigns": no login is saved, so Ghost asks for one with
// a private form, and never in the chat.
func TestAMissingSignInIsAskedForWithAPrivateForm(t *testing.T) {
	vaultFixture(t)
	var published []cards.Card
	tool := credentialsTool(&published)
	res := tool.Execute(context.Background(), map[string]interface{}{
		"site": "https://ads.google.com/home", "service": "Google Ads Manager", "reason": "Pause the campaigns that are over budget.",
	})
	if res.IsError || len(published) != 1 {
		t.Fatalf("a form should be shown: %+v published=%d", res, len(published))
	}
	c := published[0]
	if c.Kind != cards.KindCredentials || c.Title != "Sign in to Google Ads Manager" || cards.CredentialHostOf(c) != "ads.google.com" {
		t.Fatalf("card: %+v", c)
	}
	if !strings.Contains(res.ForLLM, "never see what they type") || !strings.Contains(res.ForLLM, "stop") {
		t.Fatalf("the model must be told to wait and that it cannot see the answer: %s", res.ForLLM)
	}
	// Asking again while the form is open does not stack a second one.
	again := tool.Execute(context.Background(), map[string]interface{}{"site": "ads.google.com"})
	if len(published) != 1 || !strings.Contains(again.ForLLM, "already open") {
		t.Fatalf("a second form was made: %d %s", len(published), again.ForLLM)
	}
}

func TestNoFormIsAskedForWhenALoginAlreadyExists(t *testing.T) {
	vaultFixture(t)
	if _, err := credentials.SaveItem(credentials.Item{Kind: credentials.KindLogin, URL: "https://ads.google.com", Username: "u", Password: "p-saved-pw"}); err != nil {
		t.Fatal(err)
	}
	var published []cards.Card
	res := credentialsTool(&published).Execute(context.Background(), map[string]interface{}{"site": "ads.google.com"})
	if len(published) != 0 || !strings.Contains(res.ForLLM, "already saved") || !strings.Contains(res.ForLLM, "browser_login") {
		t.Fatalf("an existing login should be used: %d %s", len(published), res.ForLLM)
	}
}

func TestOnlyAnAppThatCanShowAPrivateFormIsAskedToFillOne(t *testing.T) {
	vaultFixture(t)
	var published []cards.Card
	tool := credentialsTool(&published)
	tool.SetContext("telegram", "123")
	res := tool.Execute(context.Background(), map[string]interface{}{"site": "ads.google.com"})
	if len(published) != 0 || !strings.Contains(res.ForLLM, "Never ask for the password here") {
		t.Fatalf("a chat app must be sent to the Ghost app, not given a form: %d %s", len(published), res.ForLLM)
	}
	for _, bad := range []string{"", "localhost", "10.0.0.1", "not a site"} {
		if r := credentialsTool(&published).Execute(context.Background(), map[string]interface{}{"site": bad}); !r.IsError {
			t.Errorf("%q is not a site", bad)
		}
	}
}

// "Only filled on ads.google.com": the address the model supplies is held to the
// login's own site, so a steered model cannot send the password anywhere else.
func TestASavedLoginIsNeverTypedOnAnotherSite(t *testing.T) {
	vaultFixture(t)
	if _, err := credentials.SaveItem(credentials.Item{Kind: credentials.KindLogin, URL: "https://ads.google.com", Username: "u", Password: "p-saved-pw"}); err != nil {
		t.Fatal(err)
	}
	bt := NewBrowserTool("", "login")
	for _, url := range []string{"https://evil.example/login", "https://google.com.evil.example/", "https://ads-google.com", "http://127.0.0.1:8080/x"} {
		res := bt.Execute(context.Background(), map[string]interface{}{"host": "ads.google.com", "url": url})
		if !res.IsError || !strings.Contains(res.ForLLM, "ads.google.com") || !strings.Contains(res.ForLLM, "won't type it there") {
			t.Errorf("%s should have been refused: %+v", url, res)
		}
	}
	if !onLoginSite("ads.google.com", "https://accounts.google.com/signin") {
		t.Error("Google's own sign-in page for a Google site is the same site")
	}
	if onLoginSite("ads.google.com", "https://accounts.google.com.evil.example/") {
		t.Error("a lookalike is not the same site")
	}
}

func TestMissingLoginSendsTheModelToTheSecureForm(t *testing.T) {
	vaultFixture(t)
	res := NewBrowserTool("", "login").Execute(context.Background(), map[string]interface{}{"host": "ads.google.com"})
	if !res.IsError || !strings.Contains(res.ForLLM, "request_credentials") || !strings.Contains(res.ForLLM, "Never ask") {
		t.Fatalf("%+v", res)
	}
}

// Keys: a command gets one only when the owner allowed it, the command names it,
// and nothing from outside could have written the command.
func TestExecGivesACommandOnlyTheKeysItNamesAndMayHave(t *testing.T) {
	vaultFixture(t)
	for _, it := range []credentials.Item{
		{Kind: credentials.KindKey, Title: "Cloud", Service: "Cloud", Value: "ck_live_abcdefghijkl", EnvName: "CLOUD_TOKEN", UseInCommands: true},
		{Kind: credentials.KindKey, Title: "Stripe", Service: "Stripe", Value: "sk_live_mnopqrstuvwx", EnvName: "STRIPE_KEY"},
	} {
		if _, err := credentials.SaveItem(it); err != nil {
			t.Fatal(err)
		}
	}
	ws := t.TempDir()
	tool := NewExecTool(ws, true)
	ctx := GrantExec(context.Background(), "exec")
	run := func(ctx context.Context, cmd string) string {
		res := tool.Execute(ctx, map[string]interface{}{"command": cmd})
		if res.IsError {
			t.Fatalf("%s: %s", cmd, res.ForLLM)
		}
		return strings.TrimSpace(res.ForLLM)
	}
	if got := run(ctx, `printf %s "$CLOUD_TOKEN" | wc -c`); got != "20" {
		t.Fatalf("an allowed key the command names should arrive (length 20), got %q", got)
	}
	if got := run(ctx, `env | grep -c CLOUD_TOKEN || true`); got != "0" {
		t.Fatalf("a key must not be ambient in a command that does not name it, got %q", got)
	}
	if got := run(ctx, `printf %s "$STRIPE_KEY" | wc -c`); got != "0" {
		t.Fatalf("a key the owner did not allow must not arrive even when named, got %q", got)
	}
	tainted := WithWebDerived(ctx)
	if got := run(tainted, `printf %s "$CLOUD_TOKEN" | wc -c`); got != "0" {
		t.Fatalf("a turn that read outside content must not hand over a key, got %q", got)
	}
	if got := run(ctx, `printf %s "${CLOUD_TOKEN:-none}" | wc -c`); got != "20" {
		t.Fatalf("the braced form should work too, got %q", got)
	}
	if got := run(ctx, `CLOUD_TOKEN_2=x; printf %s "$CLOUD_TOKEN_2" | wc -c`); got != "1" {
		t.Fatalf("naming another variable must not hand over this one, got %q", got)
	}
	// And whatever a command prints is scrubbed on its way back to the model.
	reg := NewToolRegistry()
	reg.Register(tool)
	res := reg.ExecuteWithContext(ctx, "exec", map[string]interface{}{"command": `echo $CLOUD_TOKEN`}, "", "", "", nil)
	if strings.Contains(res.ForLLM, "ck_live_abcdefghijkl") {
		t.Fatalf("a key came back to the model: %s", res.ForLLM)
	}
}

func TestTheVaultToolsComeWithTheBrowser(t *testing.T) {
	reg := NewToolRegistry()
	for _, n := range []string{"browser_navigate", "browser_click", "vault_items", "request_credentials", "weather_now"} {
		reg.Register(described{n, "x"})
	}
	out := FilterToolsForTurn(reg, ProfileFull, "open https://ads.google.com and pause my campaigns", false)
	for _, want := range []string{"browser_navigate", "vault_items", "request_credentials"} {
		if _, ok := out.Get(want); !ok {
			t.Errorf("%s should be offered with the browser", want)
		}
	}
	plain := FilterToolsForTurn(reg, ProfileFull, "hello there", false)
	if _, ok := plain.Get("request_credentials"); ok {
		t.Error("a greeting should not carry the Vault")
	}
	for _, p := range []ToolProfile{ProfileMobileSafe, ProfileCoding, ProfileResearch, ProfileAdmin} {
		if !p.Allows("vault_items") || !p.Allows("request_credentials") {
			t.Errorf("profile %s should allow the Vault tools", p)
		}
	}
}

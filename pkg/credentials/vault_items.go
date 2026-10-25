package credentials

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/mail"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

// The Vault: what the owner keeps for Ghost to use on their behalf, sealed in
// the same encrypted store as every other credential. Four kinds.
//
//	login    a site, a username and a password. Ghost sees the site and the
//	         username. The password goes from here to the browser and nowhere else.
//	card     a payment card. Ghost sees its nickname, brand, last four digits
//	         and expiry. The number and the security code are never shown to it.
//	contact  a person or the owner's own details. Ghost sees all of it, so it can
//	         type a name or an address into a form.
//	key      an API key or token. Ghost sees its name. The value reaches a command
//	         only when the owner has allowed that key to and the command names it.
//
// There are three views of an item and they never mix. Item is the whole thing
// and exists only inside this package and the boundaries that fill a browser or
// a command. View is what the owner's phone is shown: no secret, ever, after the
// moment it is saved. ModelView is what Ghost may read.

// ItemKind is the kind of a Vault item.
type ItemKind string

const (
	KindLogin   ItemKind = "login"
	KindCard    ItemKind = "card"
	KindContact ItemKind = "contact"
	KindKey     ItemKind = "key"
)

// Kinds lists the kinds in the order the phone shows them.
var Kinds = []ItemKind{KindLogin, KindCard, KindContact, KindKey}

// Valid reports whether k is a kind of item.
func (k ItemKind) Valid() bool {
	for _, x := range Kinds {
		if x == k {
			return true
		}
	}
	return false
}

// Limits on what one item may hold. They keep a pasted document out of the
// vault and keep the sealed file small.
const (
	maxField     = 512
	maxNotes     = 2000
	maxKeyValue  = 8192
	maxItemsKind = 500
)

// Item is one thing in the Vault, secrets included.
type Item struct {
	ID    string   `json:"id"`
	Kind  ItemKind `json:"kind"`
	Title string   `json:"title,omitempty"`
	Notes string   `json:"notes,omitempty"`

	// login
	Host             string `json:"host,omitempty"`
	URL              string `json:"url,omitempty"`
	Username         string `json:"username,omitempty"`
	Password         string `json:"password,omitempty"`
	UsernameSelector string `json:"username_selector,omitempty"`
	PasswordSelector string `json:"password_selector,omitempty"`
	SubmitSelector   string `json:"submit_selector,omitempty"`

	// card
	Brand    string `json:"brand,omitempty"`
	Holder   string `json:"holder,omitempty"`
	Number   string `json:"number,omitempty"`
	ExpMonth int    `json:"exp_month,omitempty"`
	ExpYear  int    `json:"exp_year,omitempty"`
	CVC      string `json:"cvc,omitempty"`

	// contact
	Name    string `json:"name,omitempty"`
	Company string `json:"company,omitempty"`
	Email   string `json:"email,omitempty"`
	Phone   string `json:"phone,omitempty"`
	Street  string `json:"street,omitempty"`
	City    string `json:"city,omitempty"`
	Region  string `json:"region,omitempty"`
	Postal  string `json:"postal,omitempty"`
	Country string `json:"country,omitempty"`

	// key
	Service string `json:"service,omitempty"`
	Value   string `json:"value,omitempty"`
	// EnvName is the variable a command reads the key from, and UseInCommands
	// is the owner's yes to that. Without both, no command ever receives it.
	EnvName       string `json:"env_name,omitempty"`
	UseInCommands bool   `json:"use_in_commands,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// View is an item as the owner's phone shows it: everything except the secret,
// and which secrets are set.
type View struct {
	ID    string   `json:"id"`
	Kind  ItemKind `json:"kind"`
	Title string   `json:"title"`
	// Subtitle is the one line under the title in a list.
	Subtitle string `json:"subtitle"`
	Notes    string `json:"notes,omitempty"`

	Host     string `json:"host,omitempty"`
	URL      string `json:"url,omitempty"`
	Username string `json:"username,omitempty"`
	// HasPassword and the like say a secret is set without carrying it.
	HasPassword bool `json:"has_password,omitempty"`

	Brand    string `json:"brand,omitempty"`
	Holder   string `json:"holder,omitempty"`
	Last4    string `json:"last4,omitempty"`
	ExpMonth int    `json:"exp_month,omitempty"`
	ExpYear  int    `json:"exp_year,omitempty"`
	Expired  bool   `json:"expired,omitempty"`
	HasCVC   bool   `json:"has_cvc,omitempty"`

	Name    string `json:"name,omitempty"`
	Company string `json:"company,omitempty"`
	Email   string `json:"email,omitempty"`
	Phone   string `json:"phone,omitempty"`
	Street  string `json:"street,omitempty"`
	City    string `json:"city,omitempty"`
	Region  string `json:"region,omitempty"`
	Postal  string `json:"postal,omitempty"`
	Country string `json:"country,omitempty"`

	Service       string `json:"service,omitempty"`
	KeyHint       string `json:"key_hint,omitempty"` // last four characters, nothing more
	EnvName       string `json:"env_name,omitempty"`
	UseInCommands bool   `json:"use_in_commands,omitempty"`

	UpdatedAt time.Time `json:"updated_at"`
}

// ModelView is an item as Ghost may read it. It is deliberately smaller than
// View: a card has no holder and no expiry month it could be tricked into
// repeating, a key has no hint.
type ModelView struct {
	Kind  ItemKind `json:"kind"`
	Title string   `json:"title"`
	Line  string   `json:"line"`
}

func newItemID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("it%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

const itemIDPrefix = "vault:"

// itemStoreID is where an item lives in the sealed store. A login keeps the
// key the browser already reads (weblogin:<host>), so a login saved here and a
// login saved before the Vault existed are the same thing.
func itemStoreID(kind ItemKind, id string) string {
	if kind == KindLogin {
		return webLoginIDPrefix + NormalizeWebHost(id)
	}
	return itemIDPrefix + string(kind) + ":" + id
}

// ---------------------------------------------------------------- validation

var (
	digitsOnly = regexp.MustCompile(`\D`)
	envNameRe  = regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,63}$`)
	phoneRe    = regexp.MustCompile(`^[0-9+()\-.\s]{3,32}$`)
)

func clean(s string, max int) string {
	s = strings.TrimSpace(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, s)
	if len([]rune(s)) > max {
		s = string([]rune(s)[:max])
	}
	return s
}

// LuhnValid reports whether digits pass the Luhn check every card number does.
func LuhnValid(digits string) bool {
	if len(digits) < 12 || len(digits) > 19 {
		return false
	}
	sum, double := 0, false
	for i := len(digits) - 1; i >= 0; i-- {
		c := digits[i]
		if c < '0' || c > '9' {
			return false
		}
		d := int(c - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum%10 == 0
}

// CardBrand names the network a card number belongs to, from its first digits:
// visa, mastercard, amex, discover, diners, jcb, unionpay, verve, maestro, or
// "" when it cannot tell. The phone has the same function (lib/vault.ts) and
// both are tested against the same numbers.
func CardBrand(number string) string {
	n := digitsOnly.ReplaceAllString(number, "")
	has := func(prefixes ...string) bool {
		for _, p := range prefixes {
			if strings.HasPrefix(n, p) {
				return true
			}
		}
		return false
	}
	inRange := func(width, lo, hi int) bool {
		if len(n) < width {
			return false
		}
		var v int
		fmt.Sscanf(n[:width], "%d", &v)
		return v >= lo && v <= hi
	}
	switch {
	case has("34", "37"):
		return "amex"
	case has("4"):
		return "visa"
	case inRange(2, 51, 55) || inRange(4, 2221, 2720):
		return "mastercard"
	case inRange(6, 506099, 506198) || inRange(6, 650002, 650027):
		return "verve"
	case has("6011", "65") || inRange(3, 644, 649) || inRange(6, 622126, 622925):
		return "discover"
	case has("62"):
		return "unionpay"
	case has("36", "38", "39") || inRange(3, 300, 305):
		return "diners"
	case inRange(4, 3528, 3589):
		return "jcb"
	case has("50") || inRange(2, 56, 69):
		return "maestro"
	}
	return ""
}

func normalizeYear(y int) int {
	if y > 0 && y < 100 {
		return 2000 + y
	}
	return y
}

func expired(month, year int, now time.Time) bool {
	if month < 1 || month > 12 || year < 2000 {
		return false
	}
	end := time.Date(year, time.Month(month)+1, 1, 0, 0, 0, 0, time.UTC)
	return !now.Before(end)
}

// prepare checks an item, fills what can be worked out from the rest (host from
// URL, brand from number, a title), and cleans every field. creating says the
// item is new, so its secrets are required.
func prepare(it Item, creating bool) (Item, error) {
	if !it.Kind.Valid() {
		return Item{}, fmt.Errorf("a Vault item is a login, card, contact or key")
	}
	it.Title = clean(it.Title, 120)
	it.Notes = clean(it.Notes, maxNotes)
	switch it.Kind {
	case KindLogin:
		it.Host = NormalizeWebHost(firstNonEmpty(it.Host, it.URL))
		if it.Host == "" {
			return Item{}, fmt.Errorf("a login needs the site it is for")
		}
		if !validHostname(it.Host) {
			return Item{}, fmt.Errorf("%q is not a site address", it.Host)
		}
		it.URL = clean(it.URL, 2048)
		if it.URL == "" {
			it.URL = "https://" + it.Host
		}
		if !strings.HasPrefix(strings.ToLower(it.URL), "https://") && !strings.HasPrefix(strings.ToLower(it.URL), "http://") {
			return Item{}, fmt.Errorf("the sign-in address must start with https://")
		}
		if NormalizeWebHost(it.URL) != it.Host && !SameSite(NormalizeWebHost(it.URL), it.Host) {
			return Item{}, fmt.Errorf("the sign-in address must be on %s", it.Host)
		}
		it.Username = clean(it.Username, maxField)
		if it.Username == "" {
			return Item{}, fmt.Errorf("a login needs a username or email")
		}
		if creating && it.Password == "" {
			return Item{}, fmt.Errorf("a login needs a password")
		}
		it.UsernameSelector = clean(it.UsernameSelector, 200)
		it.PasswordSelector = clean(it.PasswordSelector, 200)
		it.SubmitSelector = clean(it.SubmitSelector, 200)
		it.ID = it.Host
		if it.Title == "" {
			it.Title = it.Host
		}
	case KindCard:
		if it.Number != "" || creating {
			n := digitsOnly.ReplaceAllString(it.Number, "")
			if !LuhnValid(n) {
				return Item{}, fmt.Errorf("that card number doesn't look right; check the digits")
			}
			it.Number = n
			it.Brand = CardBrand(n)
		}
		it.ExpYear = normalizeYear(it.ExpYear)
		if creating || it.ExpMonth != 0 || it.ExpYear != 0 {
			if it.ExpMonth < 1 || it.ExpMonth > 12 || it.ExpYear < 2000 || it.ExpYear > 2100 {
				return Item{}, fmt.Errorf("the expiry needs a month (1 to 12) and a year")
			}
		}
		if it.CVC != "" {
			it.CVC = digitsOnly.ReplaceAllString(it.CVC, "")
			if len(it.CVC) < 3 || len(it.CVC) > 4 {
				return Item{}, fmt.Errorf("a security code is 3 or 4 digits")
			}
		}
		it.Holder = clean(it.Holder, 120)
	case KindContact:
		it.Name = clean(it.Name, 160)
		it.Company = clean(it.Company, 160)
		it.Email = clean(it.Email, 254)
		it.Phone = clean(it.Phone, 32)
		it.Street, it.City, it.Region = clean(it.Street, 200), clean(it.City, 120), clean(it.Region, 120)
		it.Postal, it.Country = clean(it.Postal, 24), clean(it.Country, 80)
		if it.Name == "" && it.Company == "" {
			return Item{}, fmt.Errorf("a contact needs a name")
		}
		if it.Email != "" {
			if _, err := mail.ParseAddress(it.Email); err != nil {
				return Item{}, fmt.Errorf("that email address doesn't look right")
			}
		}
		if it.Phone != "" && !phoneRe.MatchString(it.Phone) {
			return Item{}, fmt.Errorf("a phone number has digits, spaces and + ( ) - only")
		}
		if it.Title == "" {
			it.Title = firstNonEmpty(it.Name, it.Company)
		}
	case KindKey:
		it.Service = clean(it.Service, 120)
		if it.Title == "" {
			it.Title = it.Service
		}
		if it.Title == "" {
			return Item{}, fmt.Errorf("a key needs a name")
		}
		if len(it.Value) > maxKeyValue {
			return Item{}, fmt.Errorf("that key is too long to be one")
		}
		if creating && strings.TrimSpace(it.Value) == "" {
			return Item{}, fmt.Errorf("a key needs its value")
		}
		it.Value = strings.TrimSpace(it.Value)
		it.EnvName = strings.TrimSpace(it.EnvName)
		if it.EnvName != "" && !envNameRe.MatchString(it.EnvName) {
			return Item{}, fmt.Errorf("a variable name is capital letters, digits and _, starting with a letter")
		}
		if it.UseInCommands && it.EnvName == "" {
			return Item{}, fmt.Errorf("to let commands use a key, give the variable name they read it from")
		}
	}
	return it, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

var hostnameRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$|^localhost$`)

func validHostname(h string) bool { return hostnameRe.MatchString(strings.ToLower(h)) }

// ------------------------------------------------------------------- storage

// SaveItem creates an item, or updates the one with the same ID. On an update
// a blank secret keeps the one already stored, so editing a title never asks
// for a password again.
func SaveItem(in Item) (View, error) {
	v := New(configDirForWrites())
	creating := in.ID == "" || (in.Kind != KindLogin && !v.itemExists(in.Kind, in.ID))
	if in.Kind == KindLogin {
		host := NormalizeWebHost(firstNonEmpty(in.Host, in.URL, in.ID))
		_, exists := WebLoginFor(host)
		creating = !exists
	}
	var prev Item
	havePrev := false
	if !creating {
		prev, havePrev = loadItem(in.Kind, in.ID, in.Host)
		if !havePrev {
			creating = true
		}
	}
	if havePrev {
		in = mergeSecrets(prev, in)
	}
	it, err := prepare(in, creating)
	if err != nil {
		return View{}, err
	}
	now := time.Now().UTC()
	if creating || !havePrev {
		if it.Kind != KindLogin {
			if n := countKind(it.Kind); n >= maxItemsKind {
				return View{}, fmt.Errorf("the Vault holds at most %d of these", maxItemsKind)
			}
			it.ID = newItemID()
		}
		it.CreatedAt = now
	} else {
		if it.Kind != KindLogin {
			it.ID = prev.ID
		}
		it.CreatedAt = prev.CreatedAt
	}
	it.UpdatedAt = now
	blob, err := json.Marshal(it)
	if err != nil {
		return View{}, err
	}
	if err := v.Store(itemStoreID(it.Kind, it.ID), string(blob)); err != nil {
		return View{}, err
	}
	invalidateSecretCache()
	return it.View(now), nil
}

// mergeSecrets fills the secrets an update left blank from the stored item.
func mergeSecrets(prev, in Item) Item {
	if in.Password == "" {
		in.Password = prev.Password
	}
	if in.Number == "" {
		in.Number = prev.Number
		if in.Brand == "" {
			in.Brand = prev.Brand
		}
	}
	if in.CVC == "" {
		in.CVC = prev.CVC
	}
	if in.Value == "" {
		in.Value = prev.Value
	}
	return in
}

func (v *Vault) itemExists(kind ItemKind, id string) bool {
	return v.secretValue(itemStoreID(kind, id)) != ""
}

func loadItem(kind ItemKind, id, host string) (Item, bool) {
	if kind == KindLogin {
		l, ok := WebLoginFor(firstNonEmpty(host, id))
		if !ok {
			return Item{}, false
		}
		return loginAsItem(l), true
	}
	raw := New(configDirForWrites()).secretValue(itemStoreID(kind, id))
	if raw == "" {
		return Item{}, false
	}
	var it Item
	if json.Unmarshal([]byte(raw), &it) != nil || it.Kind != kind {
		return Item{}, false
	}
	return it, true
}

// loginAsItem lifts a website login (what the browser has always read) into the
// Vault's own shape.
func loginAsItem(l WebLogin) Item {
	host := NormalizeWebHost(firstNonEmpty(l.Host, l.URL))
	return Item{
		ID: host, Kind: KindLogin, Title: firstNonEmpty(l.Title, host), Host: host, URL: l.URL,
		Username: l.Username, Password: l.Password, Notes: l.Notes,
		UsernameSelector: l.UsernameSelector, PasswordSelector: l.PasswordSelector, SubmitSelector: l.SubmitSelector,
		CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt,
	}
}

func countKind(kind ItemKind) int {
	n := 0
	for _, c := range New(configDirForWrites()).List() {
		if strings.HasPrefix(c.ID, itemIDPrefix+string(kind)+":") {
			n++
		}
	}
	return n
}

// ListItems returns the owner's view of everything in the Vault, or of one
// kind, grouped by kind and then newest first. No secret is in it.
func ListItems(kind ItemKind) []View {
	now := time.Now().UTC()
	var out []View
	for _, it := range allItems() {
		if kind != "" && it.Kind != kind {
			continue
		}
		out = append(out, it.View(now))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return kindRank(out[i].Kind) < kindRank(out[j].Kind)
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

func kindRank(k ItemKind) int {
	for i, x := range Kinds {
		if x == k {
			return i
		}
	}
	return len(Kinds)
}

// allItems reads every item out of the sealed store. It is the one place that
// does, and the callers either strip it to a View or are inside a boundary.
func allItems() []Item {
	v := New(configDirForWrites())
	var out []Item
	seenHost := map[string]bool{}
	for _, c := range v.List() {
		switch {
		case strings.HasPrefix(c.ID, webLoginIDPrefix):
			raw := v.secretValue(c.ID)
			var l WebLogin
			if raw == "" || json.Unmarshal([]byte(raw), &l) != nil {
				continue
			}
			it := loginAsItem(l)
			if it.Host == "" || seenHost[it.Host] {
				continue
			}
			seenHost[it.Host] = true
			out = append(out, it)
		case strings.HasPrefix(c.ID, itemIDPrefix):
			raw := v.secretValue(c.ID)
			var it Item
			if raw == "" || json.Unmarshal([]byte(raw), &it) != nil || !it.Kind.Valid() {
				continue
			}
			out = append(out, it)
		}
	}
	return out
}

// DeleteItem removes an item. For a login the id is its host.
func DeleteItem(kind ItemKind, id string) error {
	if !kind.Valid() {
		return fmt.Errorf("unknown kind")
	}
	err := New(configDirForWrites()).Disconnect(itemStoreID(kind, id))
	invalidateSecretCache()
	return err
}

// View strips an item to what the owner's phone is shown.
func (it Item) View(now time.Time) View {
	v := View{ID: it.ID, Kind: it.Kind, Title: it.Title, Notes: it.Notes, UpdatedAt: it.UpdatedAt}
	switch it.Kind {
	case KindLogin:
		v.Host, v.URL, v.Username, v.HasPassword = it.Host, it.URL, it.Username, it.Password != ""
		v.Subtitle = it.Username
	case KindCard:
		v.Brand, v.Holder, v.ExpMonth, v.ExpYear = it.Brand, it.Holder, it.ExpMonth, it.ExpYear
		v.Last4 = last(it.Number, 4)
		v.HasCVC = it.CVC != ""
		v.Expired = expired(it.ExpMonth, it.ExpYear, now)
		v.Subtitle = fmt.Sprintf("%s •••• %s", brandName(it.Brand), v.Last4)
		if it.ExpMonth > 0 {
			v.Subtitle += fmt.Sprintf(" · %02d/%02d", it.ExpMonth, it.ExpYear%100)
		}
	case KindContact:
		v.Name, v.Company, v.Email, v.Phone = it.Name, it.Company, it.Email, it.Phone
		v.Street, v.City, v.Region, v.Postal, v.Country = it.Street, it.City, it.Region, it.Postal, it.Country
		v.Subtitle = firstNonEmpty(it.Email, it.Phone, it.Company, it.City)
	case KindKey:
		v.Service, v.EnvName, v.UseInCommands = it.Service, it.EnvName, it.UseInCommands
		v.KeyHint = last(it.Value, 4)
		v.Subtitle = firstNonEmpty(it.Service, "Key")
		if v.KeyHint != "" {
			v.Subtitle += " · ••••" + v.KeyHint
		}
	}
	return v
}

func last(s string, n int) string {
	r := []rune(s)
	// A short secret is not hinted at: the last four of six characters is most
	// of it. Card numbers are never this short.
	if len(r) < 8 {
		return ""
	}
	return string(r[len(r)-n:])
}

func brandName(b string) string {
	switch b {
	case "visa":
		return "Visa"
	case "mastercard":
		return "Mastercard"
	case "amex":
		return "American Express"
	case "discover":
		return "Discover"
	case "diners":
		return "Diners Club"
	case "jcb":
		return "JCB"
	case "unionpay":
		return "UnionPay"
	case "verve":
		return "Verve"
	case "maestro":
		return "Maestro"
	}
	return "Card"
}

// ModelItems is what Ghost may read about the Vault: one line per item, and for
// a login only the site and the username.
func ModelItems(kind ItemKind) []ModelView {
	var out []ModelView
	for _, it := range allItems() {
		if kind != "" && it.Kind != kind {
			continue
		}
		mv := ModelView{Kind: it.Kind, Title: it.Title}
		switch it.Kind {
		case KindLogin:
			mv.Line = it.Host + " · " + it.Username
		case KindCard:
			mv.Line = fmt.Sprintf("%s ending %s", brandName(it.Brand), last(it.Number, 4))
			if expired(it.ExpMonth, it.ExpYear, time.Now().UTC()) {
				mv.Line += " (expired)"
			}
		case KindContact:
			var parts []string
			for _, p := range []string{it.Name, it.Company, it.Email, it.Phone, joinAddress(it)} {
				if p != "" {
					parts = append(parts, p)
				}
			}
			mv.Line = strings.Join(parts, " · ")
		case KindKey:
			mv.Line = firstNonEmpty(it.Service, it.Title)
			if it.UseInCommands && it.EnvName != "" {
				mv.Line += " (commands can read it as $" + it.EnvName + ")"
			}
		}
		out = append(out, mv)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return kindRank(out[i].Kind) < kindRank(out[j].Kind)
		}
		return out[i].Title < out[j].Title
	})
	return out
}

func joinAddress(it Item) string {
	var parts []string
	for _, p := range []string{it.Street, it.City, it.Region, it.Postal, it.Country} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ", ")
}

// CommandKeys returns the keys the owner has allowed commands to read, by
// variable name. Only a command that names the variable receives it (see
// KeysNamedIn); this is the whole set a caller picks from.
func CommandKeys() map[string]string {
	out := map[string]string{}
	for _, it := range allItems() {
		if it.Kind == KindKey && it.UseInCommands && it.EnvName != "" && it.Value != "" {
			out[it.EnvName] = it.Value
		}
	}
	return out
}

// KeysNamedIn picks, from the keys commands may read, those the command
// actually refers to as $NAME or ${NAME}. A key is never ambient in a command
// that does not ask for it.
func KeysNamedIn(command string) map[string]string {
	out := map[string]string{}
	for name, val := range CommandKeys() {
		// $NAME or ${NAME...}, and only that variable: $NAME_2 is another one.
		re := regexp.MustCompile(`\$\{?` + regexp.QuoteMeta(name) + `([^A-Za-z0-9_]|$)`)
		if re.MatchString(command) {
			out[name] = val
		}
	}
	return out
}

// secretFieldsOf returns every secret an item blob holds, in every spelling the
// model could be handed it in, for the scrubber at the tool-result boundary.
func secretFieldsOf(blob string) []string {
	var it Item
	if json.Unmarshal([]byte(blob), &it) != nil || !it.Kind.Valid() {
		return nil
	}
	var out []string
	switch it.Kind {
	case KindLogin:
		out = append(out, it.Password)
	case KindCard:
		out = append(out, it.Number, it.CVC)
		if n := it.Number; len(n) >= 12 {
			// A number is read back as 4242 4242 4242 4242 or 4242-4242-...
			var groups []string
			for i := 0; i < len(n); i += 4 {
				end := i + 4
				if end > len(n) {
					end = len(n)
				}
				groups = append(groups, n[i:end])
			}
			out = append(out, strings.Join(groups, " "), strings.Join(groups, "-"))
		}
	case KindKey:
		out = append(out, it.Value)
	}
	return out
}

var errNeedsLogin = fmt.Errorf("a sign-in needs a site, a username and a password")

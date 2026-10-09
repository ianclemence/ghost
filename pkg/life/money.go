package life

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Money is what the owner spends and earns, and what comes round again (a
// subscription, a bill). Amounts are kept in whole minor units (cents) with
// their currency, so nothing drifts through floating point. Nothing here is
// connected to a bank: entries come from the owner's words, a receipt photo,
// an email, or a statement they import.
type Money struct{ f *file[moneyData] }

type moneyData struct {
	Entries   []Entry     `json:"entries"`
	Recurring []Recurring `json:"recurring"`
}

// Entry is one amount spent or received.
type Entry struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"` // expense | income
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
	Merchant string `json:"merchant,omitempty"`
	Category string `json:"category"`
	Note     string `json:"note,omitempty"`
	Date     string `json:"date"` // 2006-01-02
	Source   Source `json:"source"`
}

// Recurring is something that comes round again: a subscription or a bill.
type Recurring struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"` // subscription | bill
	Name     string `json:"name"`
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
	Every    string `json:"every"` // week | month | quarter | year
	Next     string `json:"next"`  // 2006-01-02
	// Day is the day of the month it falls on, kept so the 31st stays the
	// last day of each month instead of drifting to the 1st or the 28th.
	Day      int    `json:"day,omitempty"`
	Category string `json:"category"`
	Active   bool   `json:"active"`
	Source   Source `json:"source"`
}

// Categories is the closed list spending is filed under.
var Categories = []string{"groceries", "eating out", "transport", "housing", "utilities", "health", "shopping", "entertainment", "subscriptions", "travel", "education", "family", "gifts", "fees", "savings", "income", "other"}

var everies = map[string]bool{"week": true, "month": true, "quarter": true, "year": true}

// OpenMoney opens the money store in a workspace.
func OpenMoney(workspace string) *Money {
	return &Money{f: newFile(workspace, "money.json", moneyData{})}
}

const (
	maxEntries   = 20000
	maxRecurring = 300
)

// minorDigits is how many decimal places a currency's amounts have.
func minorDigits(cur string) int {
	switch cur {
	case "JPY", "KRW", "UGX", "RWF", "VND", "CLP", "ISK", "XAF", "XOF", "TZS":
		return 0
	case "BHD", "KWD", "OMR", "JOD", "TND":
		return 3
	}
	return 2
}

var currencyRE = regexp.MustCompile(`^[A-Z]{3}$`)

// ParseAmount reads "1,250.50" in a currency as minor units. Negative and
// zero amounts are refused: the kind says which way money went.
func ParseAmount(s, currency string) (int64, error) {
	cur := strings.ToUpper(strings.TrimSpace(currency))
	if !currencyRE.MatchString(cur) {
		return 0, fmt.Errorf("%q is not a currency code (KES, USD, EUR)", currency)
	}
	t := strings.TrimSpace(strings.ReplaceAll(s, ",", ""))
	t = strings.TrimLeft(t, "$€£¥₦₹ ")
	f, err := strconv.ParseFloat(t, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("%q is not an amount", s)
	}
	if f <= 0 {
		return 0, errors.New("an amount is more than zero")
	}
	minor := math.Round(f * math.Pow10(minorDigits(cur)))
	if minor > 1e15 {
		return 0, errors.New("that amount is too large")
	}
	return int64(minor), nil
}

// FormatAmount writes minor units as "KES 1,250.50".
func FormatAmount(minor int64, currency string) string {
	dg := minorDigits(currency)
	neg := minor < 0
	if neg {
		minor = -minor
	}
	whole := minor
	frac := int64(0)
	if dg > 0 {
		p := int64(math.Pow10(dg))
		whole, frac = minor/p, minor%p
	}
	ws := strconv.FormatInt(whole, 10)
	var b strings.Builder
	for i, r := range ws {
		if i > 0 && (len(ws)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	out := currency + " " + b.String()
	if dg > 0 && frac != 0 {
		out += fmt.Sprintf(".%0*d", dg, frac)
	}
	if neg {
		return "-" + out
	}
	return out
}

func checkCategory(c string) (string, error) {
	c = strings.ToLower(strings.TrimSpace(c))
	if c == "" {
		return "other", nil
	}
	for _, x := range Categories {
		if x == c {
			return c, nil
		}
	}
	return "", fmt.Errorf("a category is one of %s, not %q", strings.Join(Categories, ", "), c)
}

// EntryInput is an amount to record.
type EntryInput struct {
	Kind     string
	Amount   string
	Currency string
	Merchant string
	Category string
	Note     string
	Date     string
	Source   Source
}

func (in EntryInput) build(now time.Time) (Entry, error) {
	e := Entry{Kind: strings.ToLower(strings.TrimSpace(in.Kind)), Currency: strings.ToUpper(strings.TrimSpace(in.Currency)), Source: in.Source}
	if e.Kind == "" {
		e.Kind = "expense"
	}
	if e.Kind != "expense" && e.Kind != "income" {
		return Entry{}, errors.New("an entry is an expense or income")
	}
	var err error
	if e.Amount, err = ParseAmount(in.Amount, e.Currency); err != nil {
		return Entry{}, err
	}
	if e.Merchant, err = text("merchant", in.Merchant, 80, false); err != nil {
		return Entry{}, err
	}
	if e.Note, err = text("note", in.Note, 200, false); err != nil {
		return Entry{}, err
	}
	if e.Category, err = checkCategory(in.Category); err != nil {
		return Entry{}, err
	}
	if e.Kind == "income" {
		e.Category = "income"
	}
	e.Date = strings.TrimSpace(in.Date)
	if e.Date == "" {
		e.Date = now.Format("2006-01-02")
	}
	d, err := time.Parse("2006-01-02", e.Date)
	if err != nil {
		return Entry{}, fmt.Errorf("%q is not a date (2026-10-12)", in.Date)
	}
	if d.After(now.AddDate(0, 0, 1)) {
		return Entry{}, errors.New("an amount spent can't be in the future (use a bill for what is coming)")
	}
	if e.Source.At.IsZero() {
		e.Source.At = now
	}
	if err := e.Source.check(); err != nil {
		return Entry{}, err
	}
	return e, nil
}

// Record keeps an amount spent or received. The same amount at the same place
// on the same day is not kept twice (a receipt read again).
func (m *Money) Record(in EntryInput, now time.Time) (Entry, bool, error) {
	e, err := in.build(now)
	if err != nil {
		return Entry{}, false, err
	}
	var dup bool
	err = m.f.with(true, func(d *moneyData) error {
		for _, x := range d.Entries {
			if sameEntry(x, e) {
				e, dup = x, true
				return nil
			}
		}
		if len(d.Entries) >= maxEntries {
			d.Entries = d.Entries[len(d.Entries)-maxEntries+1:]
		}
		e.ID = newID("money")
		d.Entries = append(d.Entries, e)
		return nil
	})
	return e, !dup, err
}

func sameEntry(a, b Entry) bool {
	return a.Kind == b.Kind && a.Amount == b.Amount && a.Currency == b.Currency && a.Date == b.Date && key(a.Merchant) == key(b.Merchant)
}

// Forget removes an entry or a recurring item by id.
func (m *Money) Forget(id string) error {
	return m.f.with(true, func(d *moneyData) error {
		for i, e := range d.Entries {
			if e.ID == id {
				d.Entries = append(d.Entries[:i], d.Entries[i+1:]...)
				return nil
			}
		}
		for i, r := range d.Recurring {
			if r.ID == id {
				d.Recurring = append(d.Recurring[:i], d.Recurring[i+1:]...)
				return nil
			}
		}
		return ErrNotFound
	})
}

// RecurringInput is a subscription or bill to keep track of.
type RecurringInput struct {
	Kind     string
	Name     string
	Amount   string
	Currency string
	Every    string
	Next     string
	Category string
	Source   Source
}

// Track keeps a subscription or bill, or updates the one with the same name.
func (m *Money) Track(in RecurringInput, now time.Time) (Recurring, bool, error) {
	r := Recurring{Kind: strings.ToLower(strings.TrimSpace(in.Kind)), Currency: strings.ToUpper(strings.TrimSpace(in.Currency)),
		Every: strings.ToLower(strings.TrimSpace(in.Every)), Next: strings.TrimSpace(in.Next), Active: true, Source: in.Source}
	if r.Kind != "subscription" && r.Kind != "bill" {
		return Recurring{}, false, errors.New("a recurring item is a subscription or a bill")
	}
	var err error
	if r.Name, err = text("name", in.Name, 80, true); err != nil {
		return Recurring{}, false, err
	}
	if r.Amount, err = ParseAmount(in.Amount, r.Currency); err != nil {
		return Recurring{}, false, err
	}
	if !everies[r.Every] {
		return Recurring{}, false, errors.New("it comes round every week, month, quarter or year")
	}
	nextDate, perr := time.Parse("2006-01-02", r.Next)
	err = perr
	if err != nil {
		return Recurring{}, false, fmt.Errorf("%q is not a date (2026-10-12)", in.Next)
	}
	r.Day = nextDate.Day()
	cat := in.Category
	if cat == "" && r.Kind == "subscription" {
		cat = "subscriptions"
	}
	if r.Category, err = checkCategory(cat); err != nil {
		return Recurring{}, false, err
	}
	if r.Source.At.IsZero() {
		r.Source.At = now
	}
	if err := r.Source.check(); err != nil {
		return Recurring{}, false, err
	}
	created := true
	err = m.f.with(true, func(d *moneyData) error {
		for i, x := range d.Recurring {
			if key(x.Name) == key(r.Name) && x.Kind == r.Kind {
				r.ID = x.ID
				d.Recurring[i] = r
				created = false
				return nil
			}
		}
		if len(d.Recurring) >= maxRecurring {
			return errors.New("Ghost already tracks the most it can")
		}
		r.ID = newID("recurring")
		d.Recurring = append(d.Recurring, r)
		return nil
	})
	return r, created, err
}

// SetActive turns tracking of a subscription or bill on or off (cancelled).
func (m *Money) SetActive(id string, active bool) (Recurring, error) {
	var out Recurring
	err := m.f.with(true, func(d *moneyData) error {
		for i := range d.Recurring {
			if d.Recurring[i].ID == id {
				d.Recurring[i].Active = active
				out = d.Recurring[i]
				return nil
			}
		}
		return ErrNotFound
	})
	return out, err
}

// Advance moves every recurring item whose date has passed to its next date,
// and returns the ones it moved (they came round).
func (m *Money) Advance(now time.Time, loc *time.Location) ([]Recurring, error) {
	var moved []Recurring
	today := now.In(loc).Format("2006-01-02")
	err := m.f.with(true, func(d *moneyData) error {
		for i := range d.Recurring {
			r := &d.Recurring[i]
			if !r.Active || r.Next >= today {
				continue
			}
			t, err := time.Parse("2006-01-02", r.Next)
			if err != nil {
				continue
			}
			anchor := r.Day
			if anchor == 0 {
				anchor = t.Day()
			}
			for guard := 0; t.Format("2006-01-02") < today && guard < 1000; guard++ {
				t = step(t, r.Every, anchor)
			}
			r.Next = t.Format("2006-01-02")
			moved = append(moved, *r)
		}
		return nil
	})
	return moved, err
}

// step moves a date on by one period. Months are counted as months: the
// anchor day is kept, or the month's last day when the month is shorter.
func step(t time.Time, every string, anchor int) time.Time {
	months := 0
	switch every {
	case "week":
		return t.AddDate(0, 0, 7)
	case "quarter":
		months = 3
	case "year":
		months = 12
	default:
		months = 1
	}
	first := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location()).AddDate(0, months, 0)
	last := first.AddDate(0, 1, -1).Day()
	d := anchor
	if d > last {
		d = last
	}
	return time.Date(first.Year(), first.Month(), d, 0, 0, 0, 0, t.Location())
}

// Recurrings lists subscriptions and bills, soonest first (active first).
func (m *Money) Recurrings() ([]Recurring, error) {
	var out []Recurring
	err := m.f.with(false, func(d *moneyData) error {
		out = append(out, d.Recurring...)
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Active != out[j].Active {
			return out[i].Active
		}
		return out[i].Next < out[j].Next
	})
	return out, err
}

// Entries lists amounts between two dates (inclusive, 2006-01-02), newest first.
func (m *Money) Entries(from, to string) ([]Entry, error) {
	var out []Entry
	err := m.f.with(false, func(d *moneyData) error {
		for _, e := range d.Entries {
			if (from == "" || e.Date >= from) && (to == "" || e.Date <= to) {
				out = append(out, e)
			}
		}
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date > out[j].Date })
	return out, err
}

// MonthSummary is one month's money in one currency.
type MonthSummary struct {
	Month      string           `json:"month"` // 2006-01
	Currency   string           `json:"currency"`
	Spent      int64            `json:"spent"`
	Earned     int64            `json:"earned"`
	Count      int              `json:"count"`
	ByCategory []CategoryTotal  `json:"by_category"`
	ByWeek     []int64          `json:"by_week"` // days 1-7, 8-14, 15-21, 22-28, 29+
	PrevSpent  int64            `json:"prev_spent"`
	Upcoming   []Recurring      `json:"upcoming"` // due in the rest of the month
	Other      map[string]int64 `json:"other_currencies,omitempty"`
}

// CategoryTotal is what was spent in one category.
type CategoryTotal struct {
	Category string `json:"category"`
	Amount   int64  `json:"amount"`
}

// Summary is a month at a glance, in the currency most of it was in (other
// currencies are counted beside it, never added to it).
func (m *Money) Summary(month string, now time.Time) (MonthSummary, error) {
	start, err := time.Parse("2006-01", month)
	if err != nil {
		return MonthSummary{}, fmt.Errorf("%q is not a month (2026-10)", month)
	}
	end := start.AddDate(0, 1, -1)
	prevStart := start.AddDate(0, -1, 0)
	prevEnd := start.AddDate(0, 0, -1)
	all, err := m.Entries(prevStart.Format("2006-01-02"), end.Format("2006-01-02"))
	if err != nil {
		return MonthSummary{}, err
	}
	counts := map[string]int{}
	for _, e := range all {
		if e.Date >= start.Format("2006-01-02") {
			counts[e.Currency]++
		}
	}
	cur := ""
	for c, n := range counts {
		if n > counts[cur] || (n == counts[cur] && c < cur) {
			cur = c
		}
	}
	s := MonthSummary{Month: month, Currency: cur, ByWeek: make([]int64, 5), Other: map[string]int64{}}
	byCat := map[string]int64{}
	for _, e := range all {
		inMonth := e.Date >= start.Format("2006-01-02")
		if !inMonth {
			if e.Currency == cur && e.Kind == "expense" && e.Date <= prevEnd.Format("2006-01-02") {
				s.PrevSpent += e.Amount
			}
			continue
		}
		if e.Currency != cur {
			if e.Kind == "expense" {
				s.Other[e.Currency] += e.Amount
			}
			continue
		}
		s.Count++
		if e.Kind == "income" {
			s.Earned += e.Amount
			continue
		}
		s.Spent += e.Amount
		byCat[e.Category] += e.Amount
		dd, _ := strconv.Atoi(e.Date[8:10])
		w := (dd - 1) / 7
		if w > 4 {
			w = 4
		}
		s.ByWeek[w] += e.Amount
	}
	for c, a := range byCat {
		s.ByCategory = append(s.ByCategory, CategoryTotal{c, a})
	}
	sort.Slice(s.ByCategory, func(i, j int) bool { return s.ByCategory[i].Amount > s.ByCategory[j].Amount })
	recs, err := m.Recurrings()
	if err != nil {
		return MonthSummary{}, err
	}
	today := now.Format("2006-01-02")
	for _, r := range recs {
		if r.Active && r.Next >= today && r.Next <= end.Format("2006-01-02") {
			s.Upcoming = append(s.Upcoming, r)
		}
	}
	if len(s.Other) == 0 {
		s.Other = nil
	}
	return s, nil
}

// ImportCSV reads a bank or mobile-money statement: a header row naming a
// date, a description and either one amount (negative = spent) or separate
// money-out and money-in columns. Rows already kept are skipped. It returns
// how many were added and skipped, and the first problem found, if any rows
// could not be read.
func (m *Money) ImportCSV(r io.Reader, currency, ref string, now time.Time) (added, skipped int, problem error) {
	cur := strings.ToUpper(strings.TrimSpace(currency))
	if !currencyRE.MatchString(cur) {
		return 0, 0, fmt.Errorf("%q is not a currency code", currency)
	}
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	header, err := cr.Read()
	if err != nil {
		return 0, 0, errors.New("the statement has no header row")
	}
	col := func(names ...string) int {
		for i, h := range header {
			hk := key(h)
			for _, n := range names {
				if hk == n || strings.Contains(hk, n) {
					return i
				}
			}
		}
		return -1
	}
	cDate := col("date", "completion time", "transaction date", "posted")
	cDesc := col("description", "details", "narrative", "merchant", "payee", "memo", "particulars")
	cAmt := col("amount", "value")
	cOut := col("withdrawn", "debit", "paid out", "money out", "withdrawal")
	cIn := col("paid in", "credit", "money in", "deposit")
	if cDate < 0 || (cAmt < 0 && cOut < 0) {
		return 0, 0, errors.New("the statement needs a date column and an amount (or money out) column")
	}
	src := Source{Kind: "import", Ref: ref, At: now}
	var entries []Entry
	line := 1
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		line++
		if err != nil {
			if problem == nil {
				problem = fmt.Errorf("line %d: %v", line, err)
			}
			continue
		}
		get := func(i int) string {
			if i < 0 || i >= len(rec) {
				return ""
			}
			return strings.TrimSpace(rec[i])
		}
		date, ok := statementDate(get(cDate))
		if !ok {
			if problem == nil {
				problem = fmt.Errorf("line %d: %q is not a date", line, get(cDate))
			}
			continue
		}
		kind, amount := "", get(cAmt)
		if cOut >= 0 && clean0(get(cOut)) != "" {
			kind, amount = "expense", get(cOut)
		} else if cIn >= 0 && clean0(get(cIn)) != "" {
			kind, amount = "income", get(cIn)
		} else if cAmt >= 0 {
			if strings.HasPrefix(strings.TrimSpace(amount), "-") || strings.HasPrefix(strings.TrimSpace(amount), "(") {
				kind = "expense"
			} else {
				kind = "income"
			}
			amount = strings.Trim(amount, "-() ")
		}
		if kind == "" {
			continue
		}
		in := EntryInput{Kind: kind, Amount: amount, Currency: cur, Merchant: truncate(get(cDesc), 80), Category: guessCategory(get(cDesc), kind), Date: date, Source: src}
		e, err := in.build(now)
		if err != nil {
			if problem == nil {
				problem = fmt.Errorf("line %d: %v", line, err)
			}
			continue
		}
		entries = append(entries, e)
	}
	err = m.f.with(true, func(d *moneyData) error {
		for _, e := range entries {
			dup := false
			for _, x := range d.Entries {
				if sameEntry(x, e) {
					dup = true
					break
				}
			}
			if dup {
				skipped++
				continue
			}
			e.ID = newID("money")
			d.Entries = append(d.Entries, e)
			added++
		}
		if len(d.Entries) > maxEntries {
			d.Entries = d.Entries[len(d.Entries)-maxEntries:]
		}
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	return added, skipped, problem
}

func clean0(s string) string {
	s = strings.Trim(strings.TrimSpace(s), "0.,-")
	return s
}

func truncate(s string, n int) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) > n {
		return string(r[:n])
	}
	return string(r)
}

// statementDate reads the date forms statements use.
func statementDate(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if len(s) > 10 && (s[10] == ' ' || s[10] == 'T') {
		s = s[:10]
	}
	for _, layout := range []string{"2006-01-02", "02/01/2006", "2/1/2006", "02-01-2006", "02.01.2006", "2006/01/02", "02 Jan 2006", "2 Jan 2006", "Jan 2, 2006", "02-Jan-2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02"), true
		}
	}
	return "", false
}

// guessCategory files a statement line by its words. It is a first guess the
// owner can change, never a claim about what was bought.
func guessCategory(desc, kind string) string {
	if kind == "income" {
		return "income"
	}
	d := strings.ToLower(desc)
	rules := []struct {
		cat   string
		words []string
	}{
		{"groceries", []string{"supermarket", "naivas", "carrefour", "quickmart", "grocer", "tesco", "walmart", "aldi", "lidl", "market"}},
		{"eating out", []string{"restaurant", "cafe", "coffee", "java", "kfc", "pizza", "burger", "glovo", "uber eats", "bolt food", "bar "}},
		{"transport", []string{"uber", "bolt", "taxi", "fuel", "petrol", "shell", "total", "rubis", "parking", "matatu", "bus", "train"}},
		{"utilities", []string{"kplc", "electric", "power", "water", "internet", "safaricom home", "zuku", "airtime", "bundles", "data"}},
		{"subscriptions", []string{"netflix", "spotify", "youtube", "apple.com", "icloud", "google storage", "showmax", "prime", "chatgpt", "openai", "subscription"}},
		{"health", []string{"pharmacy", "chemist", "hospital", "clinic", "dental", "doctor"}},
		{"housing", []string{"rent", "landlord", "mortgage"}},
		{"fees", []string{"charge", "fee", "commission", "interest"}},
		{"travel", []string{"airline", "airways", "hotel", "airbnb", "booking.com", "kenya airways", "flight"}},
		{"entertainment", []string{"cinema", "imax", "concert", "ticket", "steam", "playstation"}},
		{"shopping", []string{"jumia", "amazon", "shop", "store", "mall"}},
	}
	for _, r := range rules {
		for _, w := range r.words {
			if strings.Contains(d, w) {
				return r.cat
			}
		}
	}
	return "other"
}

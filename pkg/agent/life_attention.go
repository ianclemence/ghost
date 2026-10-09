package agent

import (
	"fmt"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/attention"
	"github.com/ianclemence/ghost/pkg/life"
)

// offerLife brings up what the owner's own records say is coming: a birthday,
// someone they asked to stay in touch with, a document about to run out, a
// bill or a renewal. Each goes through the attention layer (said once, quiet
// hours kept, small things gathered into the morning message), and each
// carries the next step the owner is likely to want, as a tap.
func (al *AgentLoop) offerLife(now time.Time, loc *time.Location) {
	if al == nil || al.workspace == "" {
		return
	}
	q := al.attentionQueue()
	for _, it := range lifeItems(al.workspace, now, loc) {
		q.Offer(it, now)
	}
}

// lifeItems is what the records say deserves a mention now. Pure apart from
// reading the stores (and rolling recurring dates forward), so it is testable.
func lifeItems(workspace string, now time.Time, loc *time.Location) []attention.Item {
	local := now.In(loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	var out []attention.Item

	// Birthdays: a week ahead (time to find a present), the day before, the day.
	if people, err := life.PeopleFor(workspace).All(); err == nil {
		for _, p := range people {
			next, age, ok := life.NextBirthday(p, now, loc)
			if !ok {
				continue
			}
			days := int(next.Sub(today).Hours() / 24)
			who := personWord(p)
			turning := ""
			if age > 0 {
				turning = fmt.Sprintf(" (turning %d)", age)
			}
			base := fmt.Sprintf("life:birthday:%s:%d", p.ID, next.Year())
			switch days {
			case 7, 6:
				out = append(out, attention.Item{Key: base + ":week", Source: "birthday", Priority: 5,
					Line:  fmt.Sprintf("%s birthday is on %s%s.", possessive(who), next.Format("Monday 2 January"), turning),
					Reply: fmt.Sprintf("Help me choose a present for %s.", p.Name), ReplyLabel: "Ideas for a present",
					Expires: next})
			case 1:
				out = append(out, attention.Item{Key: base + ":eve", Source: "birthday", Priority: 7,
					Line:  fmt.Sprintf("%s birthday is tomorrow%s.", possessive(who), turning),
					Reply: fmt.Sprintf("Draft a birthday message to %s for tomorrow.", p.Name), ReplyLabel: "Draft a message",
					Expires: next.AddDate(0, 0, 1)})
			case 0:
				out = append(out, attention.Item{Key: base + ":day", Source: "birthday", Priority: 9,
					Line:  fmt.Sprintf("It's %s birthday today%s.", possessive(who), turning),
					Reply: fmt.Sprintf("Draft a birthday message to %s.", p.Name), ReplyLabel: "Draft a message",
					Expires: next.AddDate(0, 0, 1)})
			}
			// Someone the owner asked to stay in touch with, once a week at most.
			if p.KeepInTouchDays > 0 {
				since := p.LastContact
				if since.IsZero() {
					since = p.CreatedAt
				}
				gap := int(today.Sub(since.In(loc)).Hours() / 24)
				if gap >= p.KeepInTouchDays {
					_, week := today.ISOWeek()
					out = append(out, attention.Item{Key: fmt.Sprintf("life:touch:%s:%d-%d", p.ID, today.Year(), week), Source: "keep_in_touch", Priority: 4,
						Line:  fmt.Sprintf("It's been %s since you were last in touch with %s.", spanOfDays(gap), who),
						Reply: fmt.Sprintf("Draft a short message to %s to catch up.", p.Name), ReplyLabel: "Draft a message",
						Expires: today.AddDate(0, 0, 7)})
				}
			}
		}
	}

	// Documents: three months ahead (enough to renew a passport), a month,
	// a week, and the day.
	if papers, err := life.VaultFor(workspace).All(); err == nil {
		for _, p := range papers {
			days, what, ok := life.DaysLeft(p, now, loc)
			if !ok {
				continue
			}
			stage := ""
			switch {
			case days >= 88 && days <= 90:
				stage = "90"
			case days >= 28 && days <= 30:
				stage = "30"
			case days >= 6 && days <= 7:
				stage = "7"
			case days == 0:
				stage = "0"
			}
			if stage == "" {
				continue
			}
			whose := "Your " + strings.ToLower(p.Title)
			if p.Holder != "" {
				whose = possessive(p.Holder) + " " + strings.ToLower(p.Title)
			}
			when := "today"
			if days > 0 {
				date, _ := time.ParseInLocation("2006-01-02", firstNonEmpty(p.Expires, p.Renews), loc)
				when = fmt.Sprintf("on %s (in %s)", date.Format("2 January"), spanOfDays(days))
			}
			prio := map[string]int{"90": 4, "30": 6, "7": 8, "0": 9}[stage]
			out = append(out, attention.Item{Key: fmt.Sprintf("life:paper:%s:%s:%s", p.ID, firstNonEmpty(p.Expires, p.Renews), stage), Source: "document_expiry", Priority: prio,
				Line:  fmt.Sprintf("%s %s %s.", whose, what, when),
				Reply: fmt.Sprintf("Help me renew %s.", strings.ToLower(whose)), ReplyLabel: "Help me renew",
				Expires: today.AddDate(0, 0, max(days, 0)+1)})
		}
	}

	// Bills and renewals: three days ahead, and the day before for a bill.
	money := life.MoneyFor(workspace)
	_, _ = money.Advance(now, loc)
	if recs, err := money.Recurrings(); err == nil {
		for _, r := range recs {
			if !r.Active {
				continue
			}
			due, err := time.ParseInLocation("2006-01-02", r.Next, loc)
			if err != nil {
				continue
			}
			days := int(due.Sub(today).Hours() / 24)
			amount := life.FormatAmount(r.Amount, r.Currency)
			switch {
			case days == 3:
				line := fmt.Sprintf("%s renews on %s (%s).", r.Name, due.Format("Monday"), amount)
				reply, label := fmt.Sprintf("Do I still use %s? Help me decide whether to keep it.", r.Name), "Do I still need it?"
				if r.Kind == "bill" {
					line = fmt.Sprintf("%s is due on %s (%s).", r.Name, due.Format("Monday"), amount)
					reply, label = "", ""
				}
				out = append(out, attention.Item{Key: "life:recurring:" + r.ID + ":" + r.Next + ":3", Source: "money_" + r.Kind, Priority: 5,
					Line: line, Reply: reply, ReplyLabel: label, Expires: due.AddDate(0, 0, 1)})
			case days == 1 && r.Kind == "bill":
				out = append(out, attention.Item{Key: "life:recurring:" + r.ID + ":" + r.Next + ":1", Source: "money_bill", Priority: 8,
					Line: fmt.Sprintf("%s is due tomorrow (%s).", r.Name, amount), Expires: due.AddDate(0, 0, 1)})
			}
		}
	}
	return out
}

// personWord is how the owner refers to someone: "Mum" rather than her full name.
func personWord(p life.Person) string {
	for _, a := range p.Aliases {
		switch strings.ToLower(a) {
		case "mum", "mom", "mother", "dad", "father", "grandma", "grandpa", "granny":
			return strings.ToUpper(a[:1]) + a[1:]
		}
	}
	return p.Name
}

func possessive(name string) string {
	if strings.HasSuffix(name, "s") {
		return name + "'"
	}
	return name + "'s"
}

func spanOfDays(d int) string {
	switch {
	case d == 1:
		return "a day"
	case d < 14:
		return fmt.Sprintf("%d days", d)
	case d < 60:
		return fmt.Sprintf("%d weeks", d/7)
	}
	return fmt.Sprintf("%d months", d/30)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// OwnerLocation is the owner's own time zone (from what they told Ghost), for
// anything that counts days: a birthday, an expiry, a bill.
func (al *AgentLoop) OwnerLocation() *time.Location {
	if al == nil {
		return time.Local
	}
	return al.ownerLocation()
}

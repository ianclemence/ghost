package agent

import (
	"fmt"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/attention"
	"github.com/ianclemence/ghost/pkg/jobs"
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

	// Health, once a week (Monday's morning message), only when the phone
	// shares it: the week's averages against the week before, said gently.
	// Not when the Health weekly job is on: that is the owner's week, in full.
	if local.Weekday() == time.Monday && !jobOn(workspace, "health") {
		if w, err := life.DeviceFor(workspace).Week(today.AddDate(0, 0, -1)); err == nil && w.Days >= 4 && w.Steps > 0 {
			parts := []string{fmt.Sprintf("about %s steps a day", groupThousands(w.Steps))}
			if w.PrevSteps > 0 {
				switch {
				case w.StepsChange >= 10:
					parts[0] += fmt.Sprintf(" (%.0f%% more than the week before)", w.StepsChange)
				case w.StepsChange <= -10:
					parts[0] += fmt.Sprintf(" (%.0f%% fewer than the week before)", -w.StepsChange)
				}
			}
			if w.SleepMin > 0 {
				parts = append(parts, fmt.Sprintf("%dh%02d of sleep a night", w.SleepMin/60, w.SleepMin%60))
			}
			_, wk := today.ISOWeek()
			out = append(out, attention.Item{Key: fmt.Sprintf("life:health:%d-%d", today.Year(), wk), Source: "health_weekly", Priority: 3,
				Line:  "Last week: " + strings.Join(parts, ", ") + ".",
				Reply: "Show me my week: steps and sleep, day by day.", ReplyLabel: "Show my week",
				Expires: today.AddDate(0, 0, 2)})
		}
	}

	// Trips: the week before (with a passport check), the day before a
	// flight, when to leave for it, a hotel on its day, and how it went.
	out = append(out, tripItems(workspace, now, loc, today)...)
	out = append(out, learningItems(workspace, now, today)...)

	// Bills and renewals: three days ahead, and the day before for a bill.
	fin := life.FinancesFor(workspace)
	_, _ = fin.Advance(now, loc)
	if recs, err := fin.Recurrings(); err == nil {
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
				out = append(out, attention.Item{Key: "life:recurring:" + r.ID + ":" + r.Next + ":3", Source: "finances_" + r.Kind, Priority: 5,
					Line: line, Reply: reply, ReplyLabel: label, Expires: due.AddDate(0, 0, 1)})
			case days == 1 && r.Kind == "bill":
				out = append(out, attention.Item{Key: "life:recurring:" + r.ID + ":" + r.Next + ":1", Source: "finances_bill", Priority: 8,
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

func tripItems(workspace string, now time.Time, loc *time.Location, today time.Time) []attention.Item {
	trips, err := life.TripsFor(workspace).All(now)
	if err != nil {
		return nil
	}
	papers, _ := life.VaultFor(workspace).All()
	var out []attention.Item
	for _, tr := range trips {
		start, err1 := time.ParseInLocation("2006-01-02", tr.Start, loc)
		end, err2 := time.ParseInLocation("2006-01-02", tr.End, loc)
		if err1 != nil || err2 != nil {
			continue
		}
		where := tr.Destination
		if where == "" {
			where = tr.Title
		}
		days := int(start.Sub(today).Hours() / 24)
		if days == 7 || days == 6 {
			out = append(out, attention.Item{Key: "life:trip:" + tr.ID + ":week", Source: "upcoming", Priority: 6,
				Line:       fmt.Sprintf("Your trip to %s starts on %s.", where, start.Format("Monday 2 January")),
				Reply:      fmt.Sprintf("Help me get ready for my trip to %s: the weather, what to pack and anything I still need to book.", where),
				ReplyLabel: "Help me prepare", Expires: start})
			// Many countries want six months left on a passport.
			for _, p := range papers {
				if p.Kind != "passport" || p.Holder != "" || p.Expires == "" {
					continue
				}
				exp, err := time.ParseInLocation("2006-01-02", p.Expires, loc)
				if err != nil || !exp.Before(end.AddDate(0, 6, 0)) {
					continue
				}
				out = append(out, attention.Item{Key: "life:trip:" + tr.ID + ":passport:" + p.ID, Source: "document_expiry", Priority: 8,
					Line:       fmt.Sprintf("Your passport expires on %s, less than six months after your trip to %s ends. Some countries won't let you in on it.", exp.Format("2 January 2006"), where),
					Reply:      fmt.Sprintf("Check whether my passport is valid long enough for %s, and how to renew it if not.", where),
					ReplyLabel: "Check for me", Expires: start})
			}
		}
		for i, l := range tr.Legs {
			ls, err := time.ParseInLocation(life.LegTime, l.Start, loc)
			if err != nil {
				continue
			}
			legKey := fmt.Sprintf("life:trip:%s:leg%d:%s", tr.ID, i, l.Start)
			legDay := time.Date(ls.Year(), ls.Month(), ls.Day(), 0, 0, 0, 0, loc)
			legDays := int(legDay.Sub(today).Hours() / 24)
			name := l.Title
			if l.Kind == "flight" && l.Ref != "" {
				name = l.Ref
				if l.To != "" {
					name += " to " + l.To
				}
			}
			switch {
			case l.Kind == "flight" && legDays == 1:
				it := attention.Item{Key: legKey + ":eve", Source: "upcoming", Priority: 8,
					Line:    fmt.Sprintf("Tomorrow: %s at %s. Check in online if your airline lets you.", name, ls.Format("15:04")),
					Expires: ls}
				if l.Ref != "" {
					// The tap is the owner asking, in their own words, so the
					// flight becomes a watch (gate changes, delays).
					it.Reply, it.ReplyLabel = fmt.Sprintf("Watch my flight %s tomorrow.", l.Ref), "Follow the flight"
				}
				out = append(out, it)
			case l.Kind == "hotel" && legDays == 0:
				out = append(out, attention.Item{Key: legKey + ":day", Source: "upcoming", Priority: 6,
					Line: fmt.Sprintf("Check-in at %s today from %s.", l.Title, ls.Format("15:04")), Expires: ls.Add(12 * time.Hour)})
			}
			if by, ok := life.LeaveBy(l, loc); ok && now.Before(ls) && !now.Before(by.Add(-15*time.Minute)) {
				out = append(out, attention.Item{Key: legKey + ":leave", Source: "leave_by", Priority: 10, Urgent: true, Reliable: true,
					Line:      fmt.Sprintf("Time to leave for %s at %s: be on your way by %s.", name, ls.Format("15:04"), by.Format("15:04")),
					NotBefore: by.Add(-15 * time.Minute), Expires: ls})
			}
		}
		if after := int(today.Sub(end).Hours() / 24); after == 1 || after == 2 {
			out = append(out, attention.Item{Key: "life:trip:" + tr.ID + ":after", Source: "followup", Priority: 3,
				Line: fmt.Sprintf("Welcome back from %s. How was it?", where), Expires: end.AddDate(0, 0, 4)})
		}
	}
	return out
}

func groupThousands(n int) string {
	s := fmt.Sprintf("%d", n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// learningItems: an exam or deadline a week ahead and the day before (with a
// quiz on what they kept), and, once a fortnight at most, something they were
// reading or studying that has gone quiet.
func learningItems(workspace string, now, today time.Time) []attention.Item {
	k := life.KnowledgeFor(workspace)
	all, err := k.All()
	if err != nil {
		return nil
	}
	var out []attention.Item
	for _, l := range all {
		if l.Due == "" || l.Status == "done" {
			continue
		}
		due, err := time.ParseInLocation("2006-01-02", l.Due, today.Location())
		if err != nil {
			continue
		}
		days := int(due.Sub(today).Hours() / 24)
		what := l.Title
		if l.Goal != "" {
			what = l.Goal
		}
		switch days {
		case 7:
			out = append(out, attention.Item{Key: "life:learn:" + l.ID + ":" + l.Due + ":week", Source: "upcoming", Priority: 5,
				Line:  fmt.Sprintf("A week to go: %s.", what),
				Reply: fmt.Sprintf("Help me plan the week before %s: what to go over each day.", l.Title), ReplyLabel: "Plan the week",
				Expires: today.AddDate(0, 0, 2)})
		case 1:
			out = append(out, attention.Item{Key: "life:learn:" + l.ID + ":" + l.Due + ":eve", Source: "upcoming", Priority: 8,
				Line:  fmt.Sprintf("Tomorrow: %s. Good luck.", what),
				Reply: fmt.Sprintf("Quiz me on %s: a few quick questions from my notes.", l.Title), ReplyLabel: "Quiz me",
				Expires: today.AddDate(0, 0, 1)})
		}
	}
	// One quiet thing at a time, at most once a fortnight each.
	for _, l := range k.Quiet(now, 14*24*time.Hour) {
		_, wk := today.ISOWeek()
		where := ""
		if p := l.Progress(); p != "" {
			where = " You were on " + p + "."
		}
		out = append(out, attention.Item{Key: fmt.Sprintf("life:learn:%s:quiet:%d-%d", l.ID, today.Year(), wk/2), Source: "followup", Priority: 2,
			Line:  fmt.Sprintf("Still on %s?%s", l.Title, where),
			Reply: fmt.Sprintf("Where was I with %s? Remind me what I noted.", l.Title), ReplyLabel: "Pick it up",
			Expires: today.AddDate(0, 0, 3)})
		break
	}
	return out
}

// jobOn reports whether the owner turned a job on (it then speaks for itself,
// so the attention layer does not say the same thing again).
func jobOn(workspace, id string) bool {
	states, err := jobs.Open(workspace).All()
	return err == nil && states[id].Enabled
}

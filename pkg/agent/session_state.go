package agent

import (
	"fmt"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/artifacts"
	"github.com/ianclemence/ghost/pkg/permissions"
	"github.com/ianclemence/ghost/pkg/scheduled"
)

const footerStateMarker = "[Session state @"

func (al *AgentLoop) pendingApprovalLines(session string) []string {
	if al == nil || al.governance == nil || al.governance.Broker == nil {
		return nil
	}
	var out []string
	for _, r := range al.governance.Broker.Requests(permissions.StatusPending, 50) {
		if r.SessionKey != session {
			continue
		}
		title := r.Capability
		if card, ok := r.Card(); ok && strings.TrimSpace(card.Title) != "" {
			title = card.Title
		}
		line := title
		if d := requestDetail(r); d != "" {
			line += ": " + d
		}
		if !r.ExpiresAt.IsZero() {
			line += " — expires " + r.ExpiresAt.In(time.Local).Format("15:04")
		}
		out = append(out, line)
	}
	return out
}

var detailKeys = []string{"message", "task", "title", "content", "text", "path", "to", "query", "target", "name"}

// requestDetail says what a waiting request is about, in the owner's own words
// where the call carried them.
func requestDetail(r *permissions.Request) string {
	for _, k := range detailKeys {
		if v := strings.TrimSpace(r.Continuation[k]); v != "" {
			v = strings.Join(strings.Fields(v), " ")
			if rs := []rune(v); len(rs) > 70 {
				v = string(rs[:70]) + "…"
			}
			return v
		}
	}
	return ""
}

func artifactLines(store *artifacts.Store, session string) []string {
	if store == nil {
		return nil
	}
	list, err := store.List(session, 100)
	if err != nil {
		return nil
	}
	type group struct {
		kind     string
		versions int
		latest   string
		path     string
	}
	groups := map[string]*group{}
	var order []string
	for i := len(list) - 1; i >= 0; i-- {
		a := list[i]
		g, ok := groups[a.Title]
		if !ok {
			g = &group{kind: a.Kind}
			groups[a.Title] = g
			order = append(order, a.Title)
		}
		g.versions++
		g.latest = a.Summary
		g.path = a.Path
	}
	out := make([]string, 0, len(order))
	for _, title := range order {
		g := groups[title]
		line := fmt.Sprintf("%s (%s, %d version", title, g.kind, g.versions)
		if g.versions != 1 {
			line += "s"
		}
		line += ")"
		if g.path != "" {
			line += " latest at " + g.path
		}
		if s := strings.TrimSpace(g.latest); s != "" {
			line += " — " + s
		}
		out = append(out, line)
	}
	return out
}

func (al *AgentLoop) scheduleRowLines(max int) []string {
	if al == nil || al.schedSvc == nil {
		return nil
	}
	items, err := al.schedSvc.ListItems("", scheduled.StateScheduled, 20)
	if err != nil {
		return nil
	}
	var out []string
	for _, it := range items {
		if it == nil || it.NextRunAt == nil {
			continue
		}
		when := it.NextRunAt.UTC()
		if loc, lerr := time.LoadLocation(it.Timezone); lerr == nil {
			when = it.NextRunAt.In(loc)
		}
		title := strings.TrimSpace(it.Title)
		detail := strings.TrimSpace(it.Description)
		if detail == "" {
			detail = strings.TrimSpace(it.Action.Content)
		}
		if title == "" {
			title, detail = detail, ""
		}
		if len(detail) > 90 {
			detail = strings.TrimSpace(detail[:90]) + "…"
		}
		line := fmt.Sprintf("%s → %s", title, when.Format("Mon 2006-01-02 15:04"))
		if detail != "" && !strings.EqualFold(detail, title) {
			line += " — " + detail
		}
		out = append(out, line)
		if max > 0 && len(out) >= max {
			break
		}
	}
	return out
}

func (al *AgentLoop) sessionStateFooter(session string) string {
	pending := al.pendingApprovalLines(session)
	var built []string
	if al != nil {
		built = artifactLines(al.sessionArtifacts, session)
	}
	if len(pending) == 0 && len(built) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(footerStateMarker + time.Now().Format("2006-01-02 15:04") + " — from the runtime's records, authoritative over chat text:]")
	for _, l := range pending {
		b.WriteString("\n- Waiting for approval: " + l)
	}
	for _, l := range built {
		b.WriteString("\n- Built here: " + l)
	}
	return b.String()
}

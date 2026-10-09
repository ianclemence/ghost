package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/agent"
	"github.com/ianclemence/ghost/pkg/cards"
	"github.com/ianclemence/ghost/pkg/product"
	calprov "github.com/ianclemence/ghost/pkg/providers/calendar"
	gmailprov "github.com/ianclemence/ghost/pkg/providers/gmail"
	outlookprov "github.com/ianclemence/ghost/pkg/providers/outlook"
)

// ownerConversation is the conversation the owner shares across devices.
const ownerConversation = "main"

// registerCardInputs serves the cards that ask and the drafts the owner sends.
//
//	POST /v1/cards/respond  {channel, id, answers}       answer a card that asks
//	POST /v1/cards/check    {channel, id, key, item, done} tick a checklist line
//	POST /v1/cards/draft    {channel, id, fields}         edit a draft before it goes
//
// Sending a draft is the card's "send" action on /v1/cards/resolve, carried out
// by the handler registered here: the email goes, or the event is added, and
// only then is the card marked done, with what really happened.
func registerCardInputs(mux *http.ServeMux, al *agent.AgentLoop) {
	cards.OnResolve(cards.KindDraft, func(c cards.Card, actionID string) (string, error) {
		if actionID != "send" {
			return "", errors.New("that choice can't be carried out")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		label, note, err := sendDraft(ctx, c)
		if err != nil {
			return "", err
		}
		al.NoteOwnerAction(ownerConversation, note)
		return label, nil
	})

	channelOf := func(s string) string {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
		return "mobile"
	}
	cardError := func(w http.ResponseWriter, card cards.Card, err error) {
		status := http.StatusBadRequest
		switch {
		case errors.Is(err, cards.ErrNoCard):
			status = http.StatusNotFound
		case errors.Is(err, cards.ErrResolved):
			status = http.StatusConflict
		}
		body := map[string]interface{}{"ok": false, "error": err.Error()}
		if card.ID != "" {
			body["card"] = card
		}
		jsonResponse(w, status, body)
	}

	mux.HandleFunc("/v1/cards/respond", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
			return
		}
		var req struct {
			Channel string                 `json:"channel"`
			ID      string                 `json:"id"`
			Answers map[string]interface{} `json:"answers"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 32*1024)).Decode(&req); err != nil || strings.TrimSpace(req.ID) == "" {
			jsonError(w, http.StatusBadRequest, "invalid_request", "a card id and answers are required")
			return
		}
		channel := channelOf(req.Channel)
		card, ans, err := cards.DefaultStore.Respond(channel, req.ID, req.Answers, time.Now())
		if err != nil {
			cardError(w, card, err)
			return
		}
		cards.DefaultActionLog.Record(cards.ActionRecord{CardID: card.ID, CardKind: string(card.Kind), ActionID: card.Resolved.ActionID, Actor: "owner", Result: "answered"})
		text := ans.Text
		deliver := "message"
		if card.Kind == cards.KindQuestion {
			text = cards.QuestionReply(card, ans)
			// The turn that asked is still waiting: the answer goes straight to
			// it. Otherwise it arrives as the owner's next message.
			if qid, _ := card.Data["question_id"].(string); qid != "" && al.ClarifyWaiting(qid) && al.RespondClarify(qid, text) {
				deliver = "turn"
			}
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "card": card, "text": text, "deliver": deliver})
	}))

	mux.HandleFunc("/v1/cards/check", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
			return
		}
		var req struct {
			Channel string `json:"channel"`
			ID      string `json:"id"`
			Key     string `json:"key"`
			Item    string `json:"item"`
			Done    bool   `json:"done"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil || req.ID == "" || req.Key == "" || req.Item == "" {
			jsonError(w, http.StatusBadRequest, "invalid_request", "a card id, a checklist key and an item are required")
			return
		}
		card, err := cards.DefaultStore.Check(channelOf(req.Channel), req.ID, req.Key, req.Item, req.Done)
		if err != nil {
			cardError(w, card, err)
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "card": card})
	}))

	mux.HandleFunc("/v1/cards/draft", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
			return
		}
		var req struct {
			Channel string            `json:"channel"`
			ID      string            `json:"id"`
			Fields  map[string]string `json:"fields"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil || req.ID == "" || len(req.Fields) == 0 {
			jsonError(w, http.StatusBadRequest, "invalid_request", "a card id and the changed fields are required")
			return
		}
		card, err := cards.DefaultStore.Update(channelOf(req.Channel), req.ID, func(c *cards.Card) error {
			if c.Kind != cards.KindDraft {
				return errors.New("that card is not a draft")
			}
			return cards.EditDraft(c, req.Fields)
		})
		if err != nil {
			cardError(w, card, err)
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "card": card})
	}))
}

// sendDraft carries out a draft the owner chose to send. It returns the words
// the card shows afterwards and the line Ghost reads about it; an error means
// nothing was sent, and says why in the owner's words.
func sendDraft(ctx context.Context, c cards.Card) (label, note string, err error) {
	at := time.Now().Format("15:04")
	switch cards.DraftKindOf(c) {
	case cards.DraftEmail:
		to, err := addresses(cards.DraftField(c, "to"))
		if err != nil {
			return "", "", err
		}
		cc, err := addresses(cards.DraftField(c, "cc"))
		if err != nil {
			return "", "", err
		}
		subject, body := cards.DraftField(c, "subject"), cards.DraftField(c, "body")
		if svc := gmailprov.New(gmailprov.Config{}); svc.Configured() {
			if _, r := svc.SendMail(ctx, to, cc, subject, body); r.Err != nil {
				return "", "", errors.New(product.OutcomeForProviderFailure("email", r.Failure, r.Err).UserMessage)
			}
		} else if out := outlookprov.New(outlookprov.Config{}); out.Configured() {
			if _, r := out.SendMail(ctx, to, cc, subject, body); r.Err != nil {
				return "", "", errors.New(product.OutcomeForProviderFailure("email", r.Failure, r.Err).UserMessage)
			}
		} else {
			return "", "", errors.New("No mailbox is connected. Connect Gmail or Outlook under Connected apps, then send it again.")
		}
		return "Sent " + at, fmt.Sprintf("The owner sent the email you drafted, \"%s\", to %s at %s.", subject, strings.Join(to, ", "), at), nil
	case cards.DraftEvent:
		allDay := cards.DraftField(c, "all_day") == "true"
		start, err := cards.ParseDraftTime(cards.DraftField(c, "start"), allDay)
		if err != nil {
			return "", "", err
		}
		var end time.Time
		if v := cards.DraftField(c, "end"); v != "" {
			if end, err = cards.ParseDraftTime(v, allDay); err != nil {
				return "", "", err
			}
		}
		svc := calprov.New(calprov.Config{})
		if !svc.Configured() {
			return "", "", errors.New("Your calendar isn't connected. Connect Google Calendar under Connected apps, then add it again.")
		}
		ev, r := svc.Insert(ctx, calprov.NewEvent{
			Summary: cards.DraftField(c, "subject"), Location: cards.DraftField(c, "location"),
			Notes: cards.DraftField(c, "body"), Start: start, End: end, AllDay: allDay,
		})
		if r.Err != nil {
			return "", "", errors.New(product.OutcomeForProviderFailure("calendar", r.Failure, r.Err).UserMessage)
		}
		return "Added to your calendar", fmt.Sprintf("The owner added the event you drafted, \"%s\" (%s), to their calendar.", ev.Summary, ev.Start), nil
	case cards.DraftAlarm:
		// The phone set it with the clock app's own alarm request; the Pod
		// records what the owner asked for.
		return "Set on your phone", fmt.Sprintf("The owner set the alarm you drafted for %s on their phone.", cards.DraftField(c, "start")), nil
	case cards.DraftSMS:
		// The phone opened it in Messages for the owner to send; the Pod
		// cannot know whether they did, and the card does not claim it.
		return "Opened in Messages", fmt.Sprintf("The owner opened the text you drafted to %s in their Messages app (whether they sent it is up to them).", cards.DraftField(c, "to")), nil
	}
	return "", "", errors.New("that card is not a draft")
}

func addresses(s string) ([]string, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	list, err := mail.ParseAddressList(s)
	if err != nil {
		return nil, fmt.Errorf("%q is not an email address", s)
	}
	out := make([]string, 0, len(list))
	for _, a := range list {
		out = append(out, a.Address)
	}
	return out, nil
}

// keepsReceipt reports whether an answered card of this kind stays in the
// conversation as one line of what was done.
func keepsReceipt(k cards.Kind) bool {
	switch k {
	case cards.KindPresent, cards.KindReminder, cards.KindDigest, cards.KindQuestion, cards.KindDraft:
		return true
	}
	return false
}

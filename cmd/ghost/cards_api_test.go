package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/cards"
)

func cardsMux(t *testing.T) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	registerCardInputs(mux, nil)
	return mux
}

func postJSON(t *testing.T, mux *http.ServeMux, path string, body interface{}) (int, map[string]interface{}) {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:18790"+path, strings.NewReader(string(b)))
	req.RemoteAddr = "127.0.0.1:5000" // a local program: loopback is trusted
	req.Host = "127.0.0.1:18790"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var out map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestAnsweringACardOverHTTP(t *testing.T) {
	mux := cardsMux(t)
	card, err := cards.RenderSpec([]byte(`{"kind":"present","title":"Pick a day",
		"blocks":[{"type":"choice","key":"day","label":"Day","options":[{"id":"sat","label":"Saturday"},{"id":"sun","label":"Sunday"}]}],
		"actions":[{"id":"go","label":"Send","kind":"submit"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	cards.DefaultStore.Add("http-test", card)

	code, out := postJSON(t, mux, "/v1/cards/respond", map[string]interface{}{"channel": "http-test", "id": card.ID, "answers": map[string]interface{}{"day": "mon"}})
	if code != http.StatusBadRequest || out["ok"] != false {
		t.Fatalf("a wrong answer: %d %v", code, out)
	}
	code, out = postJSON(t, mux, "/v1/cards/respond", map[string]interface{}{"channel": "http-test", "id": card.ID, "answers": map[string]interface{}{"day": "sun"}})
	if code != http.StatusOK || out["text"] != "Day: Sunday" || out["deliver"] != "message" {
		t.Fatalf("a right answer: %d %v", code, out)
	}
	code, _ = postJSON(t, mux, "/v1/cards/respond", map[string]interface{}{"channel": "http-test", "id": card.ID, "answers": map[string]interface{}{"day": "sun"}})
	if code != http.StatusConflict {
		t.Fatalf("answering twice: %d", code)
	}
	code, _ = postJSON(t, mux, "/v1/cards/respond", map[string]interface{}{"channel": "http-test", "id": "card_gone", "answers": map[string]interface{}{}})
	if code != http.StatusNotFound {
		t.Fatalf("a missing card: %d", code)
	}
}

func TestQuestionCardAnswerWithNoTurnWaitingGoesAsAMessage(t *testing.T) {
	mux := cardsMux(t)
	q, err := cards.NewQuestion("Which Sarah?", []string{"Sarah Kim", "Sarah Otieno"}, "q-1")
	if err != nil {
		t.Fatal(err)
	}
	cards.DefaultStore.Add("http-test", q)
	code, out := postJSON(t, mux, "/v1/cards/respond", map[string]interface{}{"channel": "http-test", "id": q.ID,
		"answers": map[string]interface{}{"other": "the one from work"}})
	if code != http.StatusOK || out["text"] != "the one from work" || out["deliver"] != "message" {
		t.Fatalf("question answered in words: %d %v", code, out)
	}
}

func TestChecklistAndDraftEditsOverHTTP(t *testing.T) {
	mux := cardsMux(t)
	list, _ := cards.RenderSpec([]byte(`{"kind":"present","title":"Packing","blocks":[{"type":"checklist","key":"bag","checks":[{"id":"passport","label":"Passport"}]}]}`))
	cards.DefaultStore.Add("http-test", list)
	code, out := postJSON(t, mux, "/v1/cards/check", map[string]interface{}{"channel": "http-test", "id": list.ID, "key": "bag", "item": "passport", "done": true})
	if code != http.StatusOK {
		t.Fatalf("tick: %d %v", code, out)
	}
	if got, _ := cards.DefaultStore.Find("http-test", list.ID); !got.Blocks[0].Checks[0].Done {
		t.Fatal("the tick was not kept")
	}

	draft, err := cards.NewDraft(cards.DraftSMS, map[string]string{"to": "Mum", "body": "Landed"})
	if err != nil {
		t.Fatal(err)
	}
	cards.DefaultStore.Add("http-test", draft)
	code, out = postJSON(t, mux, "/v1/cards/draft", map[string]interface{}{"channel": "http-test", "id": draft.ID, "fields": map[string]string{"body": "Landed safely, see you soon"}})
	if code != http.StatusOK {
		t.Fatalf("edit: %d %v", code, out)
	}
	code, _ = postJSON(t, mux, "/v1/cards/draft", map[string]interface{}{"channel": "http-test", "id": draft.ID, "fields": map[string]string{"subject": "nope"}})
	if code != http.StatusBadRequest {
		t.Fatalf("an sms has no subject: %d", code)
	}
	// Sending an sms draft is the phone opening Messages; the card says only that.
	h, ok := cards.HandlerFor(cards.KindDraft)
	if !ok {
		t.Fatal("no draft handler")
	}
	got, _ := cards.DefaultStore.Find("http-test", draft.ID)
	label, err := h(got, "send")
	if err != nil || label != "Opened in Messages" {
		t.Fatalf("sms send: %q %v", label, err)
	}
	if _, err := h(got, "something"); err == nil {
		t.Fatal("an unknown choice was carried out")
	}
}

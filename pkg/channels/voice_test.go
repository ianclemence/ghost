package channels

import (
	"context"
	"encoding/xml"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/voice"
)

type stubVoiceTranscriber struct {
	available bool
	text      string
	err       error
}

func (s stubVoiceTranscriber) IsAvailable() bool { return s.available }
func (s stubVoiceTranscriber) Transcribe(_ context.Context, _ string) (*voice.TranscriptionResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &voice.TranscriptionResponse{Text: s.text}, nil
}

func TestTranscribeVoiceFile(t *testing.T) {
	ok := stubVoiceTranscriber{available: true, text: "hello"}
	if got := transcribeVoiceFile(context.Background(), ok, "line", "/tmp/x.m4a", "voice"); got != "[voice transcription: hello]" {
		t.Fatalf("success marker wrong: %q", got)
	}

	failing := stubVoiceTranscriber{available: true, err: errors.New("boom")}
	if got := transcribeVoiceFile(context.Background(), failing, "sms", "/tmp/x.mp3", "voice"); got != "[voice (transcription failed)]" {
		t.Fatalf("failure marker wrong: %q", got)
	}

	down := stubVoiceTranscriber{available: false, text: "hello"}
	if got := transcribeVoiceFile(context.Background(), down, "sms", "/tmp/x.mp3", "voice"); got != "[voice]" {
		t.Fatalf("unavailable marker wrong: %q", got)
	}
	if got := transcribeVoiceFile(context.Background(), nil, "sms", "/tmp/x.mp3", "voice"); got != "[voice]" {
		t.Fatalf("nil transcriber marker wrong: %q", got)
	}
}

func TestAudioExtensionForType(t *testing.T) {
	tests := []struct {
		contentType string
		want        string
	}{
		{"audio/mpeg", ".mp3"},
		{"audio/ogg", ".ogg"},
		{"audio/opus", ".opus"},
		{"audio/mp4", ".m4a"},
		{"audio/x-m4a", ".m4a"},
		{"audio/amr", ".amr"},
		{"audio/amr-wb", ".amr"},
		{"audio/wav; charset=binary", ".wav"},
		{"audio/flac", ".flac"},
		{"image/jpeg", ""},
		{"video/mp4", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := audioExtensionForType(tt.contentType); got != tt.want {
			t.Errorf("audioExtensionForType(%q) = %q, want %q", tt.contentType, got, tt.want)
		}
	}
}

func TestParseSMSMedia(t *testing.T) {
	form := url.Values{}
	form.Set("NumMedia", "2")
	form.Set("MediaUrl0", "https://api.twilio.com/xxx/Media/AAA")
	form.Set("MediaContentType0", "audio/mpeg")
	form.Set("MediaUrl1", "https://api.twilio.com/xxx/Media/BBB")
	form.Set("MediaContentType1", "image/jpeg")
	media := parseSMSMedia(form)
	if len(media) != 2 {
		t.Fatalf("got %d media, want 2", len(media))
	}
	if media[0].contentType != "audio/mpeg" || !strings.HasPrefix(media[1].contentType, "image/") {
		t.Fatalf("wrong parse: %+v", media)
	}

	if parseSMSMedia(url.Values{}) != nil {
		t.Fatal("no NumMedia must yield nil")
	}
	bad := url.Values{}
	bad.Set("NumMedia", "not-a-number")
	if parseSMSMedia(bad) != nil {
		t.Fatal("bad NumMedia must yield nil")
	}
}

func TestWeChatVoiceXML(t *testing.T) {
	raw := `<xml><ToUserName><![CDATA[corp]]></ToUserName>` +
		`<FromUserName><![CDATA[user1]]></FromUserName>` +
		`<CreateTime>1700000000</CreateTime>` +
		`<MsgType><![CDATA[voice]]></MsgType>` +
		`<MediaId><![CDATA[media123]]></MediaId>` +
		`<Format><![CDATA[amr]]></Format>` +
		`<MsgId>98765</MsgId></xml>`
	var msg wechatXMLMessage
	if err := xml.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatal(err)
	}
	if msg.MsgType != "voice" || msg.MediaId != "media123" || msg.Format != "amr" || msg.MsgId != 98765 {
		t.Fatalf("wrong parse: %+v", msg)
	}
}

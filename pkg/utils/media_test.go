package utils

import "testing"

func TestIsAudioFile(t *testing.T) {
	tests := []struct {
		name        string
		filename    string
		contentType string
		want        bool
	}{
		{"mp3 by extension", "note.mp3", "", true},
		{"ogg voice note", "voice.ogg", "", true},
		{"opus voice note", "ptt.opus", "", true},
		{"oga container", "audio.oga", "", true},
		{"m4a line note", "audio.m4a", "", true},
		{"amr wechat note", "voice.amr", "", true},
		{"wav", "x.WAV", "", true},
		{"audio mime", "noext", "audio/mpeg", true},
		{"ogg mime", "noext", "application/ogg", true},
		{"image by extension", "photo.jpg", "", false},
		{"image mime", "file", "image/jpeg", false},
		{"video is not audio", "clip.mp4", "video/mp4", false},
		{"empty", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsAudioFile(tt.filename, tt.contentType); got != tt.want {
				t.Errorf("IsAudioFile(%q, %q) = %v, want %v", tt.filename, tt.contentType, got, tt.want)
			}
		})
	}
}

package skills

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParsePrerequisitesFromYAML(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "inline list not last line",
			content: "name: weather\nprerequisites:\n  commands: [curl]\nhomepage: https://wttr.in\n",
			want:    []string{"curl"},
		},
		{
			name:    "inline list as last frontmatter line",
			content: "name: crypto\nprerequisites:\n  commands: [curl]",
			want:    []string{"curl"},
		},
		{
			name:    "multiple inline commands",
			content: "prerequisites:\n  commands: [git, tmux, curl]",
			want:    []string{"git", "tmux", "curl"},
		},
		{
			name:    "block list form",
			content: "prerequisites:\n  commands:\n    - git\n    - tmux",
			want:    []string{"git", "tmux"},
		},
		{
			name:    "no prerequisites",
			content: "name: ascii-art\ndescription: something\n",
			want:    nil,
		},
		{
			name:    "empty commands list",
			content: "prerequisites:\n  commands: []",
			want:    nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parsePrerequisitesFromYAML(tc.content)
			gotCmds := got.Commands
			if tc.want == nil {
				if len(gotCmds) != 0 {
					t.Fatalf("expected no commands, got %v", gotCmds)
				}
				return
			}
			if !reflect.DeepEqual(gotCmds, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, gotCmds)
			}
		})
	}
}

func TestParsePrerequisites_JSONBackCompat(t *testing.T) {
	// The JSON path is checked first in parsePrerequisites; ensure it still works
	// for a SKILL.md whose frontmatter is JSON.
	path := filepath.Join(t.TempDir(), "SKILL.md")
	body := "---\n{\"name\":\"x\",\"prerequisites\":{\"commands\":[\"curl\",\"git\"]}}\n---\n# body\n"
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	got := parsePrerequisites(path)
	if !reflect.DeepEqual(got.Commands, []string{"curl", "git"}) {
		t.Fatalf("expected JSON prereqs to parse, got %v", got.Commands)
	}
}

package main

import "testing"

func TestValidInstallSkillName(t *testing.T) {
	for _, ok := range []string{"my-skill", "research_2", "a"} {
		if !validInstallSkillName(ok) {
			t.Fatalf("expected valid %q", ok)
		}
	}
	for _, bad := range []string{"../evil", "a/b", ".hidden", "has space"} {
		if validInstallSkillName(bad) {
			t.Fatalf("expected invalid %q", bad)
		}
	}
}

func TestParseSkillFrontmatter(t *testing.T) {
	n, d := parseSkillFrontmatter("---\nname: foo\ndescription: Does things\n---\nbody")
	if n != "foo" || d != "Does things" {
		t.Fatalf("got %q %q", n, d)
	}
	if n2, _ := parseSkillFrontmatter("no frontmatter"); n2 != "" {
		t.Fatalf("expected empty")
	}
}

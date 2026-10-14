package archtest

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var bannedComment = []struct {
	re   *regexp.Regexp
	why  string
	then string
}{
	{regexp.MustCompile(`(?i)\bfor now\b`), "a temporary arrangement", "do the real thing, or return an explicit error"},
	{regexp.MustCompile(`\b(TODO|FIXME|XXX)\b`), "unfinished work", "finish it, or delete the path"},
	{regexp.MustCompile(`(?i)\bwork-?arounds?\b|\b(hacks?|kludge)\b`), "a workaround", "fix the cause, or make the constraint a type or a test"},
	{regexp.MustCompile(`(?i)\b(seen live|observed live|in the wild|seen in production)\b`), "incident history", "state the rule; the regression test records the incident"},
	{regexp.MustCompile(`(?i)\b(it|this|that|which|they|we|was|were) used to\b`), "what the code used to do", "state what it does"},
	{regexp.MustCompile(`(?i)\b(old|previous|former|earlier) (behaviou?r|version|implementation|design)\b`), "history", "state what it does"},
	{regexp.MustCompile(`\bv?0\.2\d\.\d+\b`), "a release number", "state the rule, not the release that taught it"},
}

// A comment explains what the code cannot say for itself. History, stopgaps
// and release numbers rot, and an agent copies whatever it finds nearby, so
// they stay out of the source: the commit log keeps the history, a test
// keeps the regression, and a type or an explicit error keeps the constraint.
func TestComments_NoHistoryOrStopgaps(t *testing.T) {
	root := repoRoot(t)
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "build", "dist", "node_modules", "workspace":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "comments_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		for i, line := range strings.Split(string(src), "\n") {
			text := strings.TrimSpace(line)
			if !strings.HasPrefix(text, "//") {
				continue
			}
			for _, b := range bannedComment {
				if b.re.MatchString(text) {
					t.Errorf("%s:%d: comment holds %s (%s): %s", rel, i+1, b.why, b.then, text)
				}
			}
		}
		return nil
	})
}

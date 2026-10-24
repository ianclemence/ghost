package tools

import (
	"regexp"
	"strings"
	"unicode"
)

// CodeWorkIntent reports whether a message is about working on code: cloning or
// reading a repository, building, testing, fixing a project. It decides only
// whether the command tool is OFFERED for the turn; the permission broker still
// asks before anything runs.
//
// It does not depend on the owner writing English. The strongest signals are
// shapes no language changes (a repository address, a git command, a source
// file name). Beyond those it knows the words for "repository" and "clone" in
// the languages owners write in. Anything it misses is still reachable: exec is
// in the on-demand catalogue and find_tools can ask for it.
func CodeWorkIntent(msg string) bool {
	lower := strings.ToLower(strings.TrimSpace(msg))
	if lower == "" {
		return false
	}
	if forgeAddressRE.MatchString(lower) || devCommandRE.MatchString(lower) || sourceFileRE.MatchString(lower) {
		return true
	}
	for _, s := range codeSubstrings {
		if strings.Contains(lower, s) {
			return true
		}
	}
	for _, tok := range strings.FieldsFunc(lower, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' }) {
		if codeTokens[tok] {
			return true
		}
		for _, p := range codeTokenPrefixes {
			if strings.HasPrefix(tok, p) {
				return true
			}
		}
	}
	return false
}

// A code host, a .git address or an ssh remote: the same in every language.
var forgeAddressRE = regexp.MustCompile(`(?:github\.com|gitlab\.com|bitbucket\.org|codeberg\.org|sr\.ht|sourceforge\.net|git@[\w.-]+:|[\w./:-]+\.git\b)`)

// Tools whose names are the same whatever language the owner writes in.
var devCommandRE = regexp.MustCompile(`(?:^|[\s\x60])(?:git\s+\w+|npm\s+\w+|pnpm\s+\w+|yarn\s+(?:add|install|build|test)|pip3?\s+install|cargo\s+(?:build|test|run)|go\s+(?:build|test|run|mod|vet|install)|make\s+\w+|docker\s+(?:build|run|compose)|pytest|gradle|mvn\s+\w+)\b`)

// A file named like source code or a project manifest ("main.go", "app.py").
var sourceFileRE = regexp.MustCompile(`(?:^|[\s/\x60"'(])[\w.-]+\.(?:go|py|rs|tsx?|jsx|java|kt|cpp|cs|rb|php|swift|sh)\b|\b(?:go\.mod|package\.json|cargo\.toml|makefile|dockerfile|requirements\.txt)\b`)

// Words and scripts written without spaces between words, matched inside the
// message. Each is specific enough to mean a repository or a clone.
var codeSubstrings = []string{
	"github", "gitlab", "bitbucket", "codeberg",
	// Japanese, Chinese, Korean
	"リポジトリ", "レポジトリ", "クローン", "ソースコード",
	"仓库", "代码库", "代码仓", "克隆", "源代码", "代碼庫", "程式碼庫", "克隆倉庫",
	"저장소", "레포", "클론", "소스 코드", "소스코드",
	// Thai
	"คลังโค้ด", "ที่เก็บโค้ด", "โคลน", "รีโป", "ซอร์สโค้ด",
	// Arabic, Hindi
	"مستودع", "استنساخ", "الكود المصدري",
	"रिपॉजिटरी", "रिपॉज़िटरी", "क्लोन", "सोर्स कोड",
}

// Whole words in spaced scripts. Short ones are matched exactly so "repo" never
// fires inside "report".
var codeTokens = map[string]bool{
	"git": true, "repo": true, "repos": true, "codebase": true, "refactor": true, "refactoring": true,
	"commit": true, "commits": true, "compile": true,
	"репо": true, "репа": true,
	"repoyu": true, "reposunu": true,
}

// Stems that carry the idea across inflections: repository / repositorio /
// repositório / repozytorium / репозиторий, clone / clonar / clonare / klonen /
// клонировать, and the loan-words that follow them.
var codeTokenPrefixes = []string{
	"reposit", "repozytor", "репозитор",
	"clone", "cloning", "clonar", "clonag", "clonare", "cloner",
	"klon", "клонир", "клонув", "клонов",
	"αποθετήρ", "κλωνο",
}

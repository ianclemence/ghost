package rag

import (
	"strings"
	"unicode"
)

const rrfK = 60.0

// ftsQuery turns free text into an FTS5 query that matches any of its words.
// Every word is quoted, so punctuation and FTS operators in the owner's
// message are plain text.
func ftsQuery(text string) string {
	seen := map[string]bool{}
	var terms []string
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if (len([]rune(w)) < 3 && !hasDigit(w)) || ftsStop[w] || seen[w] {
			continue
		}
		seen[w] = true
		terms = append(terms, `"`+w+`"`)
		if len(terms) == 12 {
			break
		}
	}
	return strings.Join(terms, " OR ")
}

func hasDigit(w string) bool {
	for _, r := range w {
		if unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

var ftsStop = map[string]bool{
	"the": true, "and": true, "for": true, "are": true, "was": true, "you": true, "what": true, "when": true,
	"where": true, "who": true, "how": true, "did": true, "does": true, "has": true, "have": true, "with": true,
	"that": true, "this": true, "about": true, "tell": true, "can": true, "from": true, "your": true, "his": true,
	"her": true, "its": true, "they": true, "them": true, "any": true, "not": true, "but": true, "all": true,
}

// lexicalIDs returns chunk ids that share words with the query, best first.
// Exact names, numbers and codes are what meaning-based search misses.
func (s *Store) lexicalIDs(query string, n int) []string {
	q := ftsQuery(query)
	if q == "" || n <= 0 {
		return nil
	}
	rows, err := s.db.Query(`SELECT id FROM memory_chunks_fts WHERE memory_chunks_fts MATCH ? ORDER BY bm25(memory_chunks_fts) LIMIT ?`, q, n)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// fuseRanks merges two ranked id lists by reciprocal rank, so a chunk both
// searches like ranks above one only a single search found, and neither
// list's raw scores have to be comparable. Best first, no duplicates.
func fuseRanks(a, b []string) []string {
	score := map[string]float64{}
	first := map[string]int{}
	order := 0
	add := func(list []string) {
		for i, id := range list {
			score[id] += 1.0 / (rrfK + float64(i+1))
			if _, ok := first[id]; !ok {
				first[id] = order
				order++
			}
		}
	}
	add(a)
	add(b)
	ids := make([]string, 0, len(score))
	for id := range score {
		ids = append(ids, id)
	}
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0; j-- {
			x, y := ids[j-1], ids[j]
			if score[y] > score[x] || (score[y] == score[x] && first[y] < first[x]) {
				ids[j-1], ids[j] = y, x
			} else {
				break
			}
		}
	}
	return ids
}

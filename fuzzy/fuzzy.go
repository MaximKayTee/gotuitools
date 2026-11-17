// Package fuzzy ranks items against a typed query, case-insensitive.
//
// Search puts tiered matches first: each field in turn by prefix, word
// prefix, substring; then every query word somewhere. Items that match
// none of those but hold the query's letters in order in their first
// field follow, best Score first.
package fuzzy

import (
	"slices"
	"strings"
	"unicode/utf8"
)

// Score reports whether query's letters appear in text in order, and
// scores the match: letters next to each other and at the start of a
// word count for more, long texts less. Case-insensitive.
func Score(text, query string) (score int, ok bool) {
	text, query = strings.ToLower(text), strings.ToLower(query)
	at := 0
	for i, want := range query {
		skip := strings.IndexRune(text[at:], want)
		if skip < 0 {
			return 0, false
		}
		found := at + skip
		switch {
		case skip == 0 && i > 0:
			score += 3
		case found == 0 || isSep(text[found-1]):
			score += 2
		}
		at = found + utf8.RuneLen(want)
	}
	return score - len(text)/16, true
}

func isSep(b byte) bool {
	switch b {
	case ' ', '.', '_', '-', '/', ':':
		return true
	}
	return false
}

var sep = strings.NewReplacer("_", " ", ".", " ", "-", " ", "/", " ", ":", " ")

// Tier ranks fields against query: 0, 1, 2 for a prefix, word prefix,
// substring of the first field, 3, 4, 5 for the second, and so on;
// then every word of a multi-word query somewhere in the fields. -1
// when none match. An empty query is tier 0.
func Tier(query string, fields ...string) int {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return 0
	}
	for i, f := range fields {
		f = strings.ToLower(f)
		switch {
		case strings.HasPrefix(f, q):
			return 3 * i
		case strings.Contains(" "+sep.Replace(f), " "+q):
			return 3*i + 1
		case strings.Contains(f, q), strings.Contains(sep.Replace(f), q):
			return 3*i + 2
		}
	}
	words := strings.Fields(q)
	if len(words) < 2 {
		return -1
	}
	hay := strings.ToLower(strings.Join(fields, " "))
	for _, w := range words {
		if !strings.Contains(hay, w) {
			return -1
		}
	}
	return 3 * len(fields)
}

// Search returns the indexes of items matching query, best first;
// ties keep item order. fields gives an item's fields, most important
// first. An empty query returns every index.
func Search(n int, query string, fields func(i int) []string) []int {
	type hit struct{ i, tier, score int }
	var hits []hit
	for i := range n {
		f := fields(i)
		if t := Tier(query, f...); t >= 0 {
			hits = append(hits, hit{i, t, 0})
		} else if len(f) > 0 {
			if s, ok := Score(f[0], strings.ReplaceAll(query, " ", "")); ok {
				hits = append(hits, hit{i, 1 << 30, s})
			}
		}
	}
	slices.SortStableFunc(hits, func(a, b hit) int {
		if a.tier != b.tier {
			return a.tier - b.tier
		}
		return b.score - a.score
	})
	out := make([]int, len(hits))
	for k, h := range hits {
		out[k] = h.i
	}
	return out
}

// Filter is Search over a slice.
func Filter[T any](items []T, query string, fields func(T) []string) []T {
	idx := Search(len(items), query, func(i int) []string { return fields(items[i]) })
	out := make([]T, len(idx))
	for k, i := range idx {
		out[k] = items[i]
	}
	return out
}

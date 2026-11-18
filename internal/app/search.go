package app

import (
	"context"
	"sort"
	"strings"
	"unicode"

	"git.lanjak.net/rufi/dbox/internal/api"
)

// The DepotBox search is a plain substring match, so "fresh woman" finds
// nothing for "FreshWomen". When a search has no results, closeMatches runs a
// few broader searches (the term without spaces, each word, a shorter prefix)
// and keeps the names that are within a small edit distance of the term.
func (a *App) closeMatches(ctx context.Context, req api.SearchRequest) (api.SearchResponse, error) {
	want := normalize(req.SearchTerm)
	if len(want) < 4 || isDigits(want) {
		return api.SearchResponse{}, nil
	}
	seen := map[api.AppID]bool{}
	type scored struct {
		g    api.Game
		dist int
	}
	var found []scored
	limit := max(1, len([]rune(want))/4)
	for _, term := range broaderTerms(req.SearchTerm) {
		r := req
		r.SearchTerm, r.Limit = term, 100
		res, err := a.Client.Search(ctx, r)
		if err != nil {
			return api.SearchResponse{}, err
		}
		for _, g := range res.Games {
			if seen[g.AppID] {
				continue
			}
			seen[g.AppID] = true
			if d := fuzzyDistance(want, normalize(g.Name)); d <= limit {
				found = append(found, scored{g, d})
			}
		}
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].dist != found[j].dist {
			return found[i].dist < found[j].dist
		}
		return len(found[i].g.Name) < len(found[j].g.Name)
	})
	out := api.SearchResponse{Approximate: true}
	for i, f := range found {
		if i == max(req.Limit, 25) {
			break
		}
		out.Games = append(out.Games, f.g)
	}
	return out, nil
}

// broaderTerms returns up to four search terms that are wider than term.
func broaderTerms(term string) []string {
	words := strings.Fields(strings.ToLower(term))
	joined := strings.Join(words, "")
	var terms []string
	add := func(t string) {
		if len([]rune(t)) >= 3 && t != strings.ToLower(strings.TrimSpace(term)) && !contains(terms, t) && len(terms) < 4 {
			terms = append(terms, t)
		}
	}
	add(joined)
	for _, w := range words {
		add(w)
	}
	r := []rune(joined)
	add(string(r[:max(4, (len(r)*2+2)/3)]))
	return terms
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// normalize keeps only lower-case letters and digits.
func normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isDigits(s string) bool {
	return strings.TrimFunc(s, unicode.IsDigit) == ""
}

// fuzzyDistance is the smallest number of single-character edits that turn q
// into some part of name (Sellers' approximate substring match).
func fuzzyDistance(q, name string) int {
	qr, nr := []rune(q), []rune(name)
	prev := make([]int, len(qr)+1)
	for i := range prev {
		prev[i] = i
	}
	best := prev[len(qr)]
	cur := make([]int, len(qr)+1)
	for j := 1; j <= len(nr); j++ {
		cur[0] = 0
		for i := 1; i <= len(qr); i++ {
			cost := 1
			if qr[i-1] == nr[j-1] {
				cost = 0
			}
			cur[i] = min(prev[i-1]+cost, prev[i]+1, cur[i-1]+1)
		}
		best = min(best, cur[len(qr)])
		prev, cur = cur, prev
	}
	return best
}

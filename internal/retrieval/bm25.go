package retrieval

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"
	"unicode"
)

type indexedDocument struct {
	doc    KnowledgeDocument
	tokens []string
	freq   map[string]int
}
type BM25 struct {
	documents []indexedDocument
	df        map[string]int
	avgLength float64
	k1, b     float64
}

func NewBM25(documents []KnowledgeDocument) (*BM25, error) {
	if len(documents) == 0 {
		return nil, errors.New("knowledge corpus is empty")
	}
	index := &BM25{df: map[string]int{}, k1: 1.5, b: .75}
	seenIDs := map[string]bool{}
	total := 0
	for _, doc := range documents {
		if doc.DocumentID == "" || doc.Title == "" || doc.Source == "" {
			return nil, errors.New("knowledge document requires id, title, and source")
		}
		if seenIDs[doc.DocumentID] {
			return nil, errors.New("duplicate knowledge document id")
		}
		seenIDs[doc.DocumentID] = true
		text := doc.Title + " " + doc.Content + " " + strings.Join(doc.Tags, " ") + " " + doc.Service + " " + strings.Join(doc.ErrorPatterns, " ")
		tokens := tokenize(text)
		freq := map[string]int{}
		unique := map[string]bool{}
		for _, token := range tokens {
			freq[token]++
			unique[token] = true
		}
		for token := range unique {
			index.df[token]++
		}
		index.documents = append(index.documents, indexedDocument{doc: doc, tokens: tokens, freq: freq})
		total += len(tokens)
	}
	index.avgLength = float64(total) / float64(len(index.documents))
	return index, nil
}
func (b *BM25) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 5
	}
	if limit > 50 {
		limit = 50
	}
	queryTokens := tokenize(query)
	unique := map[string]bool{}
	for _, token := range queryTokens {
		unique[token] = true
	}
	var out []Result
	n := float64(len(b.documents))
	for _, document := range b.documents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		score := 0.0
		var matched []string
		length := float64(len(document.tokens))
		for token := range unique {
			tf := float64(document.freq[token])
			if tf == 0 {
				continue
			}
			df := float64(b.df[token])
			idf := math.Log(1 + (n-df+.5)/(df+.5))
			score += idf * (tf * (b.k1 + 1)) / (tf + b.k1*(1-b.b+b.b*length/b.avgLength))
			matched = append(matched, token)
		}
		if score > 0 {
			sort.Strings(matched)
			out = append(out, Result{DocumentID: document.doc.DocumentID, Score: math.Round(score*1000) / 1000, MatchedTerms: matched, Source: document.doc.Source, Title: document.doc.Title})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].DocumentID < out[j].DocumentID
		}
		return out[i].Score > out[j].Score
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func tokenize(value string) []string {
	var out []string
	var word []rune
	var previousHan rune
	flush := func() {
		if len(word) > 0 {
			out = append(out, strings.ToLower(string(word)))
			word = nil
		}
	}
	for _, r := range []rune(value) {
		switch {
		case unicode.Is(unicode.Han, r):
			flush()
			out = append(out, string(r))
			if previousHan != 0 {
				out = append(out, string([]rune{previousHan, r}))
			}
			previousHan = r
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-':
			word = append(word, unicode.ToLower(r))
			previousHan = 0
		default:
			flush()
			previousHan = 0
		}
	}
	flush()
	return out
}

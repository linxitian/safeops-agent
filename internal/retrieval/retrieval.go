package retrieval

import "context"

type KnowledgeDocument struct {
	DocumentID    string   `yaml:"document_id" json:"document_id"`
	DocumentType  string   `yaml:"document_type" json:"document_type"`
	Title         string   `yaml:"title" json:"title"`
	Content       string   `yaml:"content" json:"content"`
	Tags          []string `yaml:"tags" json:"tags"`
	Service       string   `yaml:"service" json:"service"`
	ErrorPatterns []string `yaml:"error_patterns" json:"error_patterns"`
	Source        string   `yaml:"source" json:"source"`
	Version       string   `yaml:"version" json:"version"`
}
type Result struct {
	DocumentID   string   `json:"document_id"`
	Score        float64  `json:"score"`
	MatchedTerms []string `json:"matched_terms"`
	Source       string   `json:"source"`
	Title        string   `json:"title"`
}
type Retriever interface {
	Search(context.Context, string, int) ([]Result, error)
}

package rca

import (
	"fmt"
	"math"
)

type Level string

const (
	D1 Level = "D1"
	D2 Level = "D2"
	D3 Level = "D3"
)

type ConfidenceComponents struct {
	SignalMatch      float64 `json:"signal_match"`
	LogPatternMatch  float64 `json:"log_pattern_match"`
	GraphConsistency float64 `json:"graph_consistency"`
	CaseSimilarity   float64 `json:"case_similarity"`
}

func (c ConfidenceComponents) Score() float64 {
	return round(clamp(c.SignalMatch)*.4 + clamp(c.LogPatternMatch)*.3 + clamp(c.GraphConsistency)*.2 + clamp(c.CaseSimilarity)*.1)
}

type CandidateCause struct {
	Cause        string   `json:"cause"`
	Score        float64  `json:"score"`
	EvidenceRefs []string `json:"evidence_refs"`
}
type Result struct {
	DiagnosisLevel       Level                `json:"diagnosis_level"`
	RootCause            string               `json:"root_cause,omitempty"`
	Confidence           float64              `json:"confidence"`
	ConfidenceComponents ConfidenceComponents `json:"confidence_components"`
	CandidateCauses      []CandidateCause     `json:"candidate_causes"`
	Culprit              string               `json:"culprit,omitempty"`
	EvidenceRefs         []string             `json:"evidence_refs"`
	MissingEvidence      []string             `json:"missing_evidence"`
	Remediation          []string             `json:"remediation"`
}

func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
func round(v float64) float64 { return math.Round(v*1000) / 1000 }
func (c ConfidenceComponents) Reason() string {
	return fmt.Sprintf("0.4×%.2f + 0.3×%.2f + 0.2×%.2f + 0.1×%.2f", c.SignalMatch, c.LogPatternMatch, c.GraphConsistency, c.CaseSimilarity)
}

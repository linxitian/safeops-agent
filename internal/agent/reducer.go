package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"safeops-agent/internal/task"
)

const (
	MaxReplans           = 3
	MaxNoProgressRepeats = 2
	DefaultTaskTimeout   = 2 * time.Minute
)

func initializeRuntime(checkpoint *task.RuntimeCheckpoint, now time.Time) {
	if checkpoint.MaxIterations == 0 {
		checkpoint.MaxIterations = MaxIterations
	}
	if checkpoint.MaxToolCalls == 0 {
		checkpoint.MaxToolCalls = MaxToolCalls
	}
	if checkpoint.StartedAt.IsZero() {
		checkpoint.StartedAt = now.UTC()
	}
	if checkpoint.DeadlineAt.IsZero() {
		checkpoint.DeadlineAt = checkpoint.StartedAt.Add(DefaultTaskTimeout)
	}
}

func beginDecision(checkpoint *task.RuntimeCheckpoint, now time.Time) error {
	initializeRuntime(checkpoint, now)
	if !now.Before(checkpoint.DeadlineAt) {
		return errors.New("agent task deadline reached")
	}
	if checkpoint.Iterations >= checkpoint.MaxIterations {
		return fmt.Errorf("agent iteration limit reached: %d", checkpoint.MaxIterations)
	}
	checkpoint.Iterations++
	return nil
}

func reserveToolCall(checkpoint *task.RuntimeCheckpoint) error {
	if checkpoint.ToolCalls >= checkpoint.MaxToolCalls {
		return fmt.Errorf("agent tool-call limit reached: %d", checkpoint.MaxToolCalls)
	}
	checkpoint.ToolCalls++
	return nil
}

func recordReplan(checkpoint *task.RuntimeCheckpoint) error {
	checkpoint.Replans++
	if checkpoint.Replans > MaxReplans {
		return fmt.Errorf("agent replan limit reached: %d", MaxReplans)
	}
	return nil
}

func recordObservation(checkpoint *task.RuntimeCheckpoint, observation task.RuntimeObservation) error {
	digest, err := observationDigest(observation)
	if err != nil {
		return err
	}
	observation.Digest = digest
	if digest == checkpoint.LastProgressDigest {
		checkpoint.NoProgressRepeats++
	} else {
		checkpoint.NoProgressRepeats = 0
		checkpoint.LastProgressDigest = digest
	}
	checkpoint.Observations = append(checkpoint.Observations, observation)
	if checkpoint.NoProgressRepeats >= MaxNoProgressRepeats {
		return errors.New("agent stopped after three identical tool observations")
	}
	return nil
}

func observationDigest(observation task.RuntimeObservation) (string, error) {
	stable := struct {
		ServerID  string          `json:"server_id"`
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
		Result    json.RawMessage `json:"result"`
	}{observation.ServerID, observation.Tool, observation.Arguments, observation.Result}
	b, err := json.Marshal(stable)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

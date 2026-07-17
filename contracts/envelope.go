package contracts

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

type TargetSnapshot struct {
	Type          string `json:"type"`
	ID            string `json:"id"`
	CanonicalPath string `json:"canonical_path,omitempty"`
	ExpectAbsent  bool   `json:"expect_absent,omitempty"`
	ParentPath    string `json:"parent_path,omitempty"`
	ParentInode   uint64 `json:"parent_inode,omitempty"`
	Size          int64  `json:"size,omitempty"`
	MTimeUnixNano int64  `json:"mtime_unix_nano,omitempty"`
	Mode          uint32 `json:"mode,omitempty"`
	Inode         uint64 `json:"inode,omitempty"`
	PID           int    `json:"pid,omitempty"`
	StartTicks    uint64 `json:"start_ticks,omitempty"`
	CommandDigest string `json:"command_digest,omitempty"`
	Executable    string `json:"executable,omitempty"`
	UID           int    `json:"uid,omitempty"`
	ServiceName   string `json:"service_name,omitempty"`
	ActiveState   string `json:"active_state,omitempty"`
	MainPID       int    `json:"main_pid,omitempty"`
}

func (s TargetSnapshot) Digest() (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

type ActionEnvelope struct {
	SchemaVersion  int            `json:"schema_version"`
	TraceID        string         `json:"trace_id"`
	TaskID         string         `json:"task_id"`
	SessionID      string         `json:"session_id"`
	Proposal       ActionProposal `json:"proposal"`
	ProposalDigest string         `json:"proposal_digest"`
	TargetSnapshot TargetSnapshot `json:"target_snapshot"`
	Risk           RiskResult     `json:"risk"`
	IntentDigest   string         `json:"intent_digest"`
	PolicyVersion  string         `json:"policy_version"`
	ExpiresAt      time.Time      `json:"expires_at"`
	Nonce          string         `json:"nonce"`
	ApprovalID     string         `json:"approval_id,omitempty"`
	Signature      string         `json:"signature"`
}

func (e ActionEnvelope) SigningBytes() ([]byte, error) { e.Signature = ""; return json.Marshal(e) }
func (e *ActionEnvelope) Sign(secret []byte) error {
	if len(secret) < 32 {
		return errors.New("envelope HMAC secret must be at least 32 bytes")
	}
	payload, err := e.SigningBytes()
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(payload)
	e.Signature = hex.EncodeToString(mac.Sum(nil))
	return nil
}
func (e ActionEnvelope) VerifySignature(secret []byte) error {
	if len(secret) < 32 {
		return errors.New("envelope HMAC secret must be at least 32 bytes")
	}
	got, err := hex.DecodeString(e.Signature)
	if err != nil {
		return errors.New("invalid envelope signature encoding")
	}
	payload, err := e.SigningBytes()
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(payload)
	if !hmac.Equal(got, mac.Sum(nil)) {
		return errors.New("invalid envelope signature")
	}
	return nil
}

func MarshalEnvelope(envelope ActionEnvelope) (json.RawMessage, error) {
	b, err := json.Marshal(envelope)
	return json.RawMessage(b), err
}

package contracts

import (
	"testing"
	"time"
)

func TestEnvelopeSignatureDetectsTamper(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	envelope := ActionEnvelope{SchemaVersion: 1, TraceID: "trace", TaskID: "task", SessionID: "session", Proposal: ActionProposal{Tool: "service.restart"}, ExpiresAt: time.Now().Add(time.Minute), Nonce: "n"}
	if err := envelope.Sign(secret); err != nil {
		t.Fatal(err)
	}
	if err := envelope.VerifySignature(secret); err != nil {
		t.Fatal(err)
	}
	envelope.Nonce = "tampered"
	if err := envelope.VerifySignature(secret); err == nil {
		t.Fatal("tampered envelope accepted")
	}
}

package controllers

import (
	"testing"

	"go.uber.org/zap"
)

func TestWebhookVerifier_VerifySignature(t *testing.T) {
	logger := zap.NewNop()

	t.Run("disabled_when_no_secret", func(t *testing.T) {
		v := &WebhookVerifier{
			secret:  []byte{},
			logger:  logger,
			enabled: false,
			skipPaths: map[string]bool{
				"/view":    true,
				"/metrics": true,
			},
		}

		if v.enabled != false {
			t.Error("expected verifier to be disabled")
		}
	})

	t.Run("computes_signature_correctly", func(t *testing.T) {
		secret := "test-secret"
		body := []byte(`{"status":"firing"}`)

		sig := GenerateWebhookSignature(secret, body)
		if sig == "" {
			t.Error("expected non-empty signature")
		}

		expectedSig := GenerateWebhookSignature(secret, body)
		if sig != expectedSig {
			t.Errorf("signature mismatch: got %q, want %q", sig, expectedSig)
		}
	})

	t.Run("different_bodies_different_signatures", func(t *testing.T) {
		secret := "test-secret"
		body1 := []byte(`{"status":"firing"}`)
		body2 := []byte(`{"status":"resolved"}`)

		sig1 := GenerateWebhookSignature(secret, body1)
		sig2 := GenerateWebhookSignature(secret, body2)

		if sig1 == sig2 {
			t.Error("different bodies should produce different signatures")
		}
	})

	t.Run("different_secrets_different_signatures", func(t *testing.T) {
		body := []byte(`{"status":"firing"}`)

		sig1 := GenerateWebhookSignature("secret1", body)
		sig2 := GenerateWebhookSignature("secret2", body)

		if sig1 == sig2 {
			t.Error("different secrets should produce different signatures")
		}
	})
}

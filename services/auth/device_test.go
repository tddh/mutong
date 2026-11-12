package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	oauth2model "gitee.com/tddh/mutong/models/oauth2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newDeviceDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&oauth2model.DeviceCode{}, &oauth2model.OAuth2Client{}); err != nil {
		t.Fatal(err)
	}
	client := oauth2model.OAuth2Client{
		ClientID: "cli-1", Name: "cli", RedirectURIs: "[]",
		GrantTypes: "[]", ResponseTypes: "[]", Scopes: "[]",
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func TestDeviceStartValidatesClient(t *testing.T) {
	svc := NewDeviceFlowService(newDeviceDB(t), "https://mutong.example.com")
	ctx := context.Background()

	if _, err := svc.Start(ctx, "", "read"); !errors.Is(err, ErrDeviceClientRequired) {
		t.Fatalf("empty client: expected ErrDeviceClientRequired, got %v", err)
	}
	if _, err := svc.Start(ctx, "nope", "read"); !errors.Is(err, ErrDeviceInvalidClient) {
		t.Fatalf("unknown client: expected ErrDeviceInvalidClient, got %v", err)
	}
	authz, err := svc.Start(ctx, "cli-1", "read")
	if err != nil {
		t.Fatalf("valid client: %v", err)
	}
	if authz.Status != "pending" || authz.DeviceCode == "" || authz.UserCode == "" {
		t.Fatalf("unexpected authorization: %+v", authz)
	}
}

func TestDeviceVerificationURIsUseConfiguredBase(t *testing.T) {
	svc := NewDeviceFlowService(newDeviceDB(t), "https://mutong.example.com/")
	uri, complete := svc.VerificationURIs("ABCD2345")
	if uri != "https://mutong.example.com/device" {
		t.Fatalf("verification_uri must use configured base, got %q", uri)
	}
	if complete != "https://mutong.example.com/device?code=ABCD2345" {
		t.Fatalf("verification_uri_complete mismatch: %q", complete)
	}
}

func TestExternalBaseURL(t *testing.T) {
	t.Setenv("MUTONG_ISSUER_URL", "")
	if got := ExternalBaseURL(); got != "http://localhost:8888" {
		t.Fatalf("default base URL: got %q", got)
	}
	t.Setenv("MUTONG_ISSUER_URL", "https://ops.example.com/")
	if got := ExternalBaseURL(); got != "https://ops.example.com" {
		t.Fatalf("configured base URL must be trimmed: got %q", got)
	}
}

func TestDeviceApproveExchangeBindsUserAndClient(t *testing.T) {
	svc := NewDeviceFlowService(newDeviceDB(t), "")
	ctx := context.Background()
	authz, err := svc.Start(ctx, "cli-1", "read")
	if err != nil {
		t.Fatal(err)
	}

	// Before approval the exchange reports pending.
	if _, err := svc.Exchange(ctx, authz.DeviceCode, "cli-1"); !errors.Is(err, ErrDevicePending) {
		t.Fatalf("expected pending, got %v", err)
	}

	if err := svc.Approve(ctx, authz.UserCode, 7); err != nil {
		t.Fatalf("approve: %v", err)
	}

	got, err := svc.Exchange(ctx, authz.DeviceCode, "cli-1")
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if got.UserID == nil || *got.UserID != 7 {
		t.Fatalf("exchange must return the approving user, got %+v", got.UserID)
	}
	if got.Scope != "read" || got.Status != "completed" {
		t.Fatalf("unexpected authorization: %+v", got)
	}

	// Single use: a second exchange must fail.
	if _, err := svc.Exchange(ctx, authz.DeviceCode, "cli-1"); !errors.Is(err, ErrDeviceUsed) {
		t.Fatalf("expected single-use rejection, got %v", err)
	}
}

func TestDeviceExchangeRejectsClientMismatch(t *testing.T) {
	svc := NewDeviceFlowService(newDeviceDB(t), "")
	ctx := context.Background()
	authz, _ := svc.Start(ctx, "cli-1", "read")
	if err := svc.Approve(ctx, authz.UserCode, 7); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Exchange(ctx, authz.DeviceCode, "other-cli"); !errors.Is(err, ErrDeviceClientMismatch) {
		t.Fatalf("wrong client: expected mismatch, got %v", err)
	}
	if _, err := svc.Exchange(ctx, authz.DeviceCode, ""); !errors.Is(err, ErrDeviceClientMismatch) {
		t.Fatalf("empty client: expected mismatch, got %v", err)
	}
}

func TestDeviceDeny(t *testing.T) {
	svc := NewDeviceFlowService(newDeviceDB(t), "")
	ctx := context.Background()
	authz, _ := svc.Start(ctx, "cli-1", "read")
	if err := svc.Deny(ctx, authz.UserCode, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Exchange(ctx, authz.DeviceCode, "cli-1"); !errors.Is(err, ErrDeviceDenied) {
		t.Fatalf("expected denied, got %v", err)
	}
}

func TestDeviceApproveOnlyOnce(t *testing.T) {
	svc := NewDeviceFlowService(newDeviceDB(t), "")
	ctx := context.Background()
	authz, _ := svc.Start(ctx, "cli-1", "read")
	if err := svc.Approve(ctx, authz.UserCode, 7); err != nil {
		t.Fatal(err)
	}
	if err := svc.Approve(ctx, authz.UserCode, 8); !errors.Is(err, ErrDeviceUsed) {
		t.Fatalf("second approve must fail, got %v", err)
	}
}

func TestDeviceExpired(t *testing.T) {
	db := newDeviceDB(t)
	svc := NewDeviceFlowService(db, "")
	ctx := context.Background()
	authz, _ := svc.Start(ctx, "cli-1", "read")
	db.Model(&oauth2model.DeviceCode{}).Where("device_code = ?", authz.DeviceCode).
		Update("expires_at", time.Now().Add(-time.Minute))

	if err := svc.Approve(ctx, authz.UserCode, 7); !errors.Is(err, ErrDeviceExpired) {
		t.Fatalf("approve expired: expected ErrDeviceExpired, got %v", err)
	}
	if _, err := svc.Exchange(ctx, authz.DeviceCode, "cli-1"); !errors.Is(err, ErrDeviceExpired) {
		t.Fatalf("exchange expired: expected ErrDeviceExpired, got %v", err)
	}
}

package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"

	oauth2model "gitee.com/tddh/mutong/models/oauth2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Device-flow errors. Callers map these to RFC 8628 token-endpoint error codes.
var (
	ErrDeviceClientRequired = errors.New("client_id is required")
	ErrDeviceInvalidClient  = errors.New("invalid client_id")
	ErrDeviceNotFound       = errors.New("device code not found")
	ErrDeviceExpired        = errors.New("device code expired")
	ErrDevicePending        = errors.New("authorization pending")
	ErrDeviceDenied         = errors.New("authorization denied")
	ErrDeviceClientMismatch = errors.New("client_id does not match device authorization")
	ErrDeviceUsed           = errors.New("device code already used")
)

const (
	deviceCodeTTL  = 10 * time.Minute
	userCodeChars  = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	userCodeLength = 8
)

// ExternalBaseURL returns the configured public base URL for device verification
// URIs. It reads MUTONG_ISSUER_URL (the same value used for the OAuth2 issuer)
// and never derives from a request's Origin header, which an attacker controls.
func ExternalBaseURL() string {
	if u := strings.TrimSpace(os.Getenv("MUTONG_ISSUER_URL")); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "http://localhost:8888"
}

// DeviceAuthorization is the persisted state of a device grant.
type DeviceAuthorization struct {
	DeviceCode string
	UserCode   string
	ClientID   string
	Scope      string
	Status     string
	UserID     *uint
	ExpiresAt  time.Time
}

// DeviceFlowService implements the security-critical parts of RFC 8628 on top of
// the persisted DeviceCode rows: authorization is bound to the approving user
// and client, the token exchange validates that binding, and verification URIs
// come from configuration rather than the request Origin.
type DeviceFlowService struct {
	db      *gorm.DB
	baseURL string
}

// NewDeviceFlowService creates the service. An empty baseURL falls back to
// ExternalBaseURL().
func NewDeviceFlowService(db *gorm.DB, baseURL string) *DeviceFlowService {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = ExternalBaseURL()
	}
	return &DeviceFlowService{db: db, baseURL: strings.TrimRight(baseURL, "/")}
}

// VerificationURIs returns the verification_uri and verification_uri_complete
// for a user code, built from the configured external base URL.
func (s *DeviceFlowService) VerificationURIs(userCode string) (uri, complete string) {
	return s.baseURL + "/device", s.baseURL + "/device?code=" + userCode
}

// Start validates the client and persists a pending device authorization.
func (s *DeviceFlowService) Start(ctx context.Context, clientID, scope string) (*DeviceAuthorization, error) {
	if clientID == "" {
		return nil, ErrDeviceClientRequired
	}
	var client oauth2model.OAuth2Client
	if err := s.db.WithContext(ctx).Where("client_id = ?", clientID).First(&client).Error; err != nil {
		return nil, ErrDeviceInvalidClient
	}

	deviceCode, err := generateDeviceCode()
	if err != nil {
		return nil, err
	}
	userCode, err := generateUserCode()
	if err != nil {
		return nil, err
	}

	dc := &oauth2model.DeviceCode{
		DeviceCode: deviceCode,
		UserCode:   userCode,
		ClientID:   clientID,
		Scope:      scope,
		Status:     "pending",
		ExpiresAt:  time.Now().Add(deviceCodeTTL),
	}
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "device_code"}},
		DoUpdates: clause.AssignmentColumns([]string{"user_code", "client_id", "scope", "status", "expires_at"}),
	}).Create(dc).Error; err != nil {
		return nil, err
	}
	return deviceAuthorizationFrom(dc), nil
}

// Approve binds the device authorization to the authenticated user. userID must
// come from a verified identity, never from the request body.
func (s *DeviceFlowService) Approve(ctx context.Context, userCode string, userID uint) error {
	return s.decide(ctx, userCode, userID, "authorized")
}

// Deny rejects the device authorization.
func (s *DeviceFlowService) Deny(ctx context.Context, userCode string, userID uint) error {
	return s.decide(ctx, userCode, userID, "denied")
}

func (s *DeviceFlowService) decide(ctx context.Context, userCode string, userID uint, status string) error {
	if strings.TrimSpace(userCode) == "" {
		return ErrDeviceNotFound
	}
	var dc oauth2model.DeviceCode
	if err := s.db.WithContext(ctx).Where("user_code = ?", strings.ToUpper(strings.TrimSpace(userCode))).First(&dc).Error; err != nil {
		return ErrDeviceNotFound
	}
	if time.Now().After(dc.ExpiresAt) {
		return ErrDeviceExpired
	}
	if dc.Status != "pending" {
		return ErrDeviceUsed
	}
	// Only a pending grant can be decided, and the decision is bound to the user.
	res := s.db.WithContext(ctx).Model(&oauth2model.DeviceCode{}).
		Where("id = ? AND status = ?", dc.ID, "pending").
		Updates(map[string]interface{}{"status": status, "user_id": userID})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrDeviceUsed
	}
	return nil
}

// Exchange validates the persisted authorization and the client binding, then
// atomically consumes the grant (single use). It returns the authorization so the
// caller can issue a token bound to the approving user and granted scope.
func (s *DeviceFlowService) Exchange(ctx context.Context, deviceCode, clientID string) (*DeviceAuthorization, error) {
	if deviceCode == "" {
		return nil, ErrDeviceNotFound
	}
	var dc oauth2model.DeviceCode
	if err := s.db.WithContext(ctx).Where("device_code = ?", deviceCode).First(&dc).Error; err != nil {
		return nil, ErrDeviceNotFound
	}
	if time.Now().After(dc.ExpiresAt) {
		return nil, ErrDeviceExpired
	}
	// The token request must present the same client the grant was issued to.
	if clientID == "" || dc.ClientID != clientID {
		return nil, ErrDeviceClientMismatch
	}

	switch dc.Status {
	case "pending":
		return nil, ErrDevicePending
	case "denied":
		return nil, ErrDeviceDenied
	case "authorized":
		// Atomic single-use transition: only one exchange can win.
		res := s.db.WithContext(ctx).Model(&oauth2model.DeviceCode{}).
			Where("id = ? AND status = ?", dc.ID, "authorized").
			Update("status", "completed")
		if res.Error != nil {
			return nil, res.Error
		}
		if res.RowsAffected == 0 {
			return nil, ErrDeviceUsed
		}
		dc.Status = "completed"
		return deviceAuthorizationFrom(&dc), nil
	default:
		return nil, ErrDeviceUsed
	}
}

func deviceAuthorizationFrom(dc *oauth2model.DeviceCode) *DeviceAuthorization {
	return &DeviceAuthorization{
		DeviceCode: dc.DeviceCode,
		UserCode:   dc.UserCode,
		ClientID:   dc.ClientID,
		Scope:      dc.Scope,
		Status:     dc.Status,
		UserID:     dc.UserID,
		ExpiresAt:  dc.ExpiresAt,
	}
}

func generateUserCode() (string, error) {
	code := make([]byte, userCodeLength)
	for i := range code {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(userCodeChars))))
		if err != nil {
			return "", err
		}
		code[i] = userCodeChars[n.Int64()]
	}
	return string(code), nil
}

func generateDeviceCode() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b), nil
}

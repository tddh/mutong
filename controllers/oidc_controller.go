package controllers

import (
	"context"
	"crypto/subtle"
	"net/http"

	"golang.org/x/oauth2"

	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/services/auth"
	"github.com/gin-gonic/gin"
)

// oidcCookiePath is the single path used to both set and clear the one-time
// OIDC state/verifier cookies, so the browser actually removes them.
const oidcCookiePath = "/api/auth/oidc"

// oidcService defines the OIDC operations needed by the controller.
type oidcService interface {
	BuildAuthURL() (authURL, state, verifier string)
	Exchange(ctx context.Context, code, verifier string) (*oauth2.Token, *auth.ZitadelUserInfo, error)
}

// OIDCController handles OIDC login and callback endpoints.
type OIDCController struct {
	UserSvc  interfaces.UserInterface
	OIDCSvc  oidcService
	Sessions *auth.SessionManager
}

// NewOIDCController creates an OIDCController.
func NewOIDCController(userSvc interfaces.UserInterface, oidcSvc oidcService, sessions *auth.SessionManager) *OIDCController {
	return &OIDCController{UserSvc: userSvc, OIDCSvc: oidcSvc, Sessions: sessions}
}

// RegisterRoutes registers OIDC routes on the given router group.
func (oc *OIDCController) RegisterRoutes(r gin.IRouter) {
	oidc := r.Group("/api/auth/oidc")
	oidc.GET("/login", oc.Login)
	oidc.GET("/callback", oc.Callback)
}

// Login initiates the OIDC authorization flow.
func (oc *OIDCController) Login(c *gin.Context) {
	if oc.OIDCSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "OIDC provider not configured"})
		return
	}
	authURL, state, verifier := oc.OIDCSvc.BuildAuthURL()
	secure := cookieSecure(c)
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("mutong_oidc_state", state, 300, oidcCookiePath, "", secure, true)
	c.SetCookie("mutong_oidc_verifier", verifier, 300, oidcCookiePath, "", secure, true)
	c.Redirect(http.StatusFound, authURL)
}

// clearOIDCCookies removes the one-time state/verifier cookies using the same
// path they were set with. It must run before any response body is written.
func (oc *OIDCController) clearOIDCCookies(c *gin.Context) {
	secure := cookieSecure(c)
	c.SetCookie("mutong_oidc_state", "", -1, oidcCookiePath, "", secure, true)
	c.SetCookie("mutong_oidc_verifier", "", -1, oidcCookiePath, "", secure, true)
}

// Callback handles the OIDC authorization code callback.
func (oc *OIDCController) Callback(c *gin.Context) {
	stateCookie, stateErr := c.Cookie("mutong_oidc_state")
	verifier, verifierErr := c.Cookie("mutong_oidc_verifier")
	// Single-use: clear the OIDC cookies before writing any response so they are
	// removed on every exit path (success, invalid state, exchange failure).
	oc.clearOIDCCookies(c)

	// Constant-time comparison avoids leaking the expected state via timing.
	if stateErr != nil || stateCookie == "" ||
		subtle.ConstantTimeCompare([]byte(c.Query("state")), []byte(stateCookie)) != 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid state"})
		return
	}
	if verifierErr != nil || verifier == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing verifier"})
		return
	}
	code := c.Query("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing code"})
		return
	}

	_, userInfo, err := oc.OIDCSvc.Exchange(c.Request.Context(), code, verifier)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication failed"})
		return
	}

	user, err := oc.UserSvc.GetUserByZitadelID(userInfo.Subject)
	if err != nil {
		user, err = oc.UserSvc.CreateUserFromZitadel(
			userInfo.Subject,
			userInfo.PreferredName,
			userInfo.Email,
			userInfo.Name,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user"})
			return
		}
	}

	if oc.Sessions == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "session manager not configured; set MUTONG_SESSION_SECRET or auth.global_secret",
		})
		return
	}

	token, err := oc.Sessions.Issue(auth.SessionClaims{
		UserID:              user.ID,
		UUID:                user.UUID,
		Role:                user.Role,
		AuthProvider:        "zitadel",
		PasswordFingerprint: auth.PasswordFingerprint(user.PasswordHash),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "session creation failed"})
		return
	}

	maxAge := int(oc.Sessions.TTL().Seconds())
	setSessionCookie(c, token, maxAge)

	c.Redirect(http.StatusFound, "/view/index.html")
}

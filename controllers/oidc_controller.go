package controllers

import (
	"context"
	"net/http"

	"golang.org/x/oauth2"

	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/services/auth"
	"github.com/gin-gonic/gin"
)

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
	secure := c.Request.TLS != nil
	c.SetCookie("mutong_oidc_state", state, 300, "/api/auth/oidc", "", secure, true)
	c.SetCookie("mutong_oidc_verifier", verifier, 300, "/api/auth/oidc", "", secure, true)
	c.Redirect(http.StatusFound, authURL)
}

// Callback handles the OIDC authorization code callback.
func (oc *OIDCController) Callback(c *gin.Context) {
	stateCookie, err := c.Cookie("mutong_oidc_state")
	if err != nil || c.Query("state") != stateCookie {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid state"})
		return
	}
	verifier, err := c.Cookie("mutong_oidc_verifier")
	if err != nil {
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

	token, err := oc.Sessions.Issue(auth.SessionClaims{
		UserID:       user.ID,
		UUID:         user.UUID,
		Role:         user.Role,
		AuthProvider: "zitadel",
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "session creation failed"})
		return
	}

	secure := c.Request.TLS != nil
	maxAge := int(oc.Sessions.TTL().Seconds())
	c.SetCookie("mutong_session", token, maxAge, "/", "", secure, true)

	// Clear OIDC-specific cookies
	c.SetCookie("mutong_oidc_state", "", -1, "/", "", secure, true)
	c.SetCookie("mutong_oidc_verifier", "", -1, "/", "", secure, true)

	c.Redirect(http.StatusFound, "/view/index.html")
}

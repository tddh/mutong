package oauth2

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ory/fosite"
)

type DiscoveryHandler struct {
	IssuerURL string
}

func (h *DiscoveryHandler) HandleDiscovery(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"issuer":                                h.IssuerURL,
		"authorization_endpoint":                h.IssuerURL + "/oauth2/authorize",
		"token_endpoint":                        h.IssuerURL + "/oauth2/token",
		"introspection_endpoint":                h.IssuerURL + "/oauth2/introspect",
		"revocation_endpoint":                   h.IssuerURL + "/oauth2/revoke",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token", "client_credentials"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_post", "none"},
	})
}

func RegisterOAuth2Routes(r *gin.Engine, oauth2 fosite.OAuth2Provider, issuerURL string, deviceH *DeviceHandler) {
	oauth2Group := r.Group("/oauth2")
	{
		authH := &AuthorizeHandler{OAuth2: oauth2}
		oauth2Group.GET("/authorize", authH.HandleAuthorize)
		oauth2Group.POST("/authorize", authH.HandleAuthorize)

		tokenH := &TokenHandler{OAuth2: oauth2, DeviceH: deviceH}
		oauth2Group.POST("/token", tokenH.HandleToken)

		r.POST("/oauth/v2/token", tokenH.HandleToken)

		introH := &IntrospectHandler{OAuth2: oauth2}
		oauth2Group.POST("/introspect", introH.HandleIntrospect)

		oauth2Group.POST("/revoke", func(c *gin.Context) {
			ctx := c.Request.Context()
			err := oauth2.NewRevocationRequest(ctx, c.Request)
			oauth2.WriteRevocationResponse(ctx, c.Writer, err)
		})
	}

	discH := &DiscoveryHandler{IssuerURL: issuerURL}
	r.GET("/.well-known/openid-configuration", discH.HandleDiscovery)
}

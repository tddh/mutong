package oauth2

import (
	"encoding/json"
	"time"

	"github.com/ory/fosite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type OAuth2Client struct {
	ID            uint   `gorm:"primaryKey"`
	ClientID      string `gorm:"uniqueIndex;size:255;not null"`
	ClientSecret  []byte `gorm:"size:60"`
	Name          string `gorm:"size:255;not null"`
	RedirectURIs  string `gorm:"type:text;not null"`
	GrantTypes    string `gorm:"type:text;not null"`
	ResponseTypes string `gorm:"type:text;not null"`
	Scopes        string `gorm:"type:text;not null"`
	Public        bool   `gorm:"not null;default:false"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     gorm.DeletedAt `gorm:"index"`
}

func (c OAuth2Client) GetID() string {
	return c.ClientID
}

func (c OAuth2Client) GetRedirectURIs() []string {
	var uris []string
	_ = json.Unmarshal([]byte(c.RedirectURIs), &uris)
	return uris
}

func (c OAuth2Client) GetHashedSecret() []byte {
	return c.ClientSecret
}

func (c OAuth2Client) GetScopes() fosite.Arguments {
	var scopes fosite.Arguments
	_ = json.Unmarshal([]byte(c.Scopes), &scopes)
	return scopes
}

func (c OAuth2Client) GetGrantTypes() fosite.Arguments {
	var grantTypes fosite.Arguments
	_ = json.Unmarshal([]byte(c.GrantTypes), &grantTypes)
	return grantTypes
}

func (c OAuth2Client) GetResponseTypes() fosite.Arguments {
	var responseTypes fosite.Arguments
	_ = json.Unmarshal([]byte(c.ResponseTypes), &responseTypes)
	return responseTypes
}

func (c OAuth2Client) IsPublic() bool {
	return c.Public
}

func (c OAuth2Client) GetAudience() fosite.Arguments {
	return fosite.Arguments{}
}

func SeedClients(db *gorm.DB) {
	emptyHash, _ := bcrypt.GenerateFromPassword([]byte(""), bcrypt.DefaultCost)

	clients := []OAuth2Client{
		{
			ClientID:      "mutong-web",
			ClientSecret:  emptyHash,
			Name:          "Mutong Web UI",
			RedirectURIs:  `["/view/callback"]`,
			GrantTypes:    `["authorization_code","refresh_token"]`,
			ResponseTypes: `["code"]`,
			Scopes:        `["openid","profile"]`,
			Public:        true,
		},
		{
			ClientID:      "mutongctl",
			ClientSecret:  emptyHash,
			Name:          "Mutong CLI",
			RedirectURIs:  `[]`,
			GrantTypes:    `["urn:ietf:params:oauth:grant-type:device_code","refresh_token"]`,
			ResponseTypes: `[]`,
			Scopes:        `["openid","profile","read","write"]`,
			Public:        true,
		},
	}
	for _, c := range clients {
		db.Where("client_id = ?", c.ClientID).FirstOrCreate(&c)
	}
}

// Package google wraps the single "SajitKhadka Connect" Google OAuth client:
// building consent URLs, exchanging codes, refreshing access tokens, and revoking.
package google

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type Client struct {
	clientID     string
	clientSecret string
	redirectURL  string
}

func New(clientID, clientSecret, redirectURL string) *Client {
	return &Client{clientID: clientID, clientSecret: clientSecret, redirectURL: redirectURL}
}

func (c *Client) config(scopes []string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     c.clientID,
		ClientSecret: c.clientSecret,
		RedirectURL:  c.redirectURL,
		Endpoint:     google.Endpoint,
		Scopes:       scopes,
	}
}

// AuthCodeURL builds the consent URL for an app's scope set. prompt=consent on the
// first grant guarantees a refresh token; include_granted_scopes keeps prior grants.
func (c *Client) AuthCodeURL(scopes []string, state, loginHint string) string {
	return c.config(scopes).AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
		oauth2.SetAuthURLParam("include_granted_scopes", "true"),
		oauth2.SetAuthURLParam("login_hint", loginHint),
	)
}

// Exchange trades an auth code for tokens (incl. refresh token on first consent).
func (c *Client) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	return c.config(nil).Exchange(ctx, code)
}

// FreshAccessToken uses a stored refresh token to mint a current access token.
func (c *Client) FreshAccessToken(ctx context.Context, refreshToken string) (*oauth2.Token, error) {
	src := c.config(nil).TokenSource(ctx, &oauth2.Token{
		RefreshToken: refreshToken,
		Expiry:       time.Now().Add(-time.Minute), // force refresh
	})
	tok, err := src.Token()
	if err != nil {
		return nil, fmt.Errorf("refresh: %w", err)
	}
	return tok, nil
}

// Revoke revokes a refresh (or access) token at Google.
func (c *Client) Revoke(ctx context.Context, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://oauth2.googleapis.com/revoke",
		strings.NewReader(url.Values{"token": {token}}.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("google revoke returned %d", resp.StatusCode)
	}
	return nil
}

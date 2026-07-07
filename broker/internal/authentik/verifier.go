// Package authentik validates inbound access tokens issued by Authentik to subapps.
//
// Assumption (documented in the deploy README): Authentik OIDC providers are set to
// a SHARED / "global" issuer + signing key, so the broker validates one issuer/JWKS
// and derives the calling app from the token's azp (or aud) client_id.
package authentik

import (
	"context"
	"errors"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
)

type Verifier struct {
	v *oidc.IDTokenVerifier
}

// Claims we care about from a subapp's access token.
type Claims struct {
	Subject  string   // stable user id
	Email    string   // convenience for login_hint
	ClientID string   // azp (preferred) — maps to app_registry.authentik_client_id
	Audience []string // aud fallback
}

type rawClaims struct {
	Email string `json:"email"`
	Azp   string `json:"azp"`
}

// New builds a verifier against the shared JWKS. Client-ID checking is skipped
// here because aud varies per app; the caller maps ClientID -> app_id itself.
func New(ctx context.Context, issuer, jwksURL string) *Verifier {
	ks := oidc.NewRemoteKeySet(ctx, jwksURL)
	v := oidc.NewVerifier(issuer, ks, &oidc.Config{
		SkipClientIDCheck: true,
		// access tokens are JWTs here; SupportedSigningAlgs defaults are fine
	})
	return &Verifier{v: v}
}

// Verify checks signature + iss + expiry and returns the mapped claims.
func (v *Verifier) Verify(ctx context.Context, rawToken string) (*Claims, error) {
	tok, err := v.v.Verify(ctx, rawToken)
	if err != nil {
		return nil, fmt.Errorf("token verify: %w", err)
	}
	if tok.Subject == "" {
		return nil, errors.New("token missing sub")
	}
	var rc rawClaims
	if err := tok.Claims(&rc); err != nil {
		return nil, fmt.Errorf("parse claims: %w", err)
	}
	clientID := rc.Azp
	if clientID == "" && len(tok.Audience) > 0 {
		clientID = tok.Audience[0]
	}
	return &Claims{
		Subject:  tok.Subject,
		Email:    rc.Email,
		ClientID: clientID,
		Audience: tok.Audience,
	}, nil
}

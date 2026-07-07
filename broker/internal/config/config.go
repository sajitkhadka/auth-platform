// Package config loads broker configuration from the environment.
package config

import (
	"fmt"
	"os"
)

type Config struct {
	// HTTP
	Port    string
	BaseURL string // public origin, e.g. https://connect.sajitkhadka.com

	// Storage
	DatabaseURL string

	// Secrets
	TokenEncKey   string // base64 std, 32 bytes -> AES-256-GCM for refresh tokens
	SessionSecret string // signs the broker's own session cookie

	// The broker's OWN Authentik OIDC client (so it knows the logged-in user).
	AuthentikIssuer        string // e.g. https://auth.sajitkhadka.com/application/o/connect/
	BrokerOIDCClientID     string
	BrokerOIDCClientSecret string

	// Validation of INBOUND subapp access tokens on POST /token.
	// Assumes Authentik providers use a shared ("global") issuer + signing key,
	// so the broker validates one issuer/JWKS and keys app identity off azp/aud.
	AuthentikTokenIssuer string // iss claim on subapp tokens
	AuthentikJWKSURL     string // JWKS endpoint holding the shared signing key(s)

	// The single Google OAuth client ("SajitKhadka Connect").
	GoogleClientID     string
	GoogleClientSecret string
}

func FromEnv() (*Config, error) {
	c := &Config{
		Port:                   getenv("PORT", "8080"),
		BaseURL:                os.Getenv("BASE_URL"),
		DatabaseURL:            os.Getenv("DATABASE_URL"),
		TokenEncKey:            os.Getenv("TOKEN_ENC_KEY"),
		SessionSecret:          os.Getenv("SESSION_SECRET"),
		AuthentikIssuer:        os.Getenv("AUTHENTIK_ISSUER"),
		BrokerOIDCClientID:     os.Getenv("BROKER_OIDC_CLIENT_ID"),
		BrokerOIDCClientSecret: os.Getenv("BROKER_OIDC_CLIENT_SECRET"),
		AuthentikTokenIssuer:   os.Getenv("AUTHENTIK_TOKEN_ISSUER"),
		AuthentikJWKSURL:       os.Getenv("AUTHENTIK_JWKS_URL"),
		GoogleClientID:         os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret:     os.Getenv("GOOGLE_CLIENT_SECRET"),
	}
	for k, v := range map[string]string{
		"BASE_URL":              c.BaseURL,
		"DATABASE_URL":          c.DatabaseURL,
		"TOKEN_ENC_KEY":         c.TokenEncKey,
		"SESSION_SECRET":        c.SessionSecret,
		"AUTHENTIK_ISSUER":      c.AuthentikIssuer,
		"BROKER_OIDC_CLIENT_ID": c.BrokerOIDCClientID,
		"AUTHENTIK_JWKS_URL":    c.AuthentikJWKSURL,
		"GOOGLE_CLIENT_ID":      c.GoogleClientID,
		"GOOGLE_CLIENT_SECRET":  c.GoogleClientSecret,
	} {
		if v == "" {
			return nil, fmt.Errorf("missing required env %s", k)
		}
	}
	return c, nil
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

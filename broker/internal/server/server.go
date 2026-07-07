// Package server wires the broker's HTTP endpoints.
package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/sajitkhadka/auth-platform/broker/internal/authentik"
	"github.com/sajitkhadka/auth-platform/broker/internal/config"
	"github.com/sajitkhadka/auth-platform/broker/internal/crypto"
	googlecli "github.com/sajitkhadka/auth-platform/broker/internal/google"
	"github.com/sajitkhadka/auth-platform/broker/internal/session"
	"github.com/sajitkhadka/auth-platform/broker/internal/store"
)

type Server struct {
	cfg      *config.Config
	store    *store.Store
	cipher   *crypto.Cipher
	verifier *authentik.Verifier // inbound subapp tokens
	google   *googlecli.Client
	signer   *session.Signer
	log      *slog.Logger

	// broker's OWN Authentik login (so it knows the user)
	oauth      *oauth2.Config
	idVerifier *oidc.IDTokenVerifier
}

func New(ctx context.Context, cfg *config.Config, st *store.Store, log *slog.Logger) (*Server, error) {
	cipher, err := crypto.New(cfg.TokenEncKey)
	if err != nil {
		return nil, err
	}
	provider, err := oidc.NewProvider(ctx, cfg.AuthentikIssuer)
	if err != nil {
		return nil, err
	}
	oauthCfg := &oauth2.Config{
		ClientID:     cfg.BrokerOIDCClientID,
		ClientSecret: cfg.BrokerOIDCClientSecret,
		RedirectURL:  cfg.BaseURL + "/auth/callback",
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
	}
	return &Server{
		cfg:        cfg,
		store:      st,
		cipher:     cipher,
		verifier:   authentik.New(ctx, cfg.AuthentikTokenIssuer, cfg.AuthentikJWKSURL),
		google:     googlecli.New(cfg.GoogleClientID, cfg.GoogleClientSecret, cfg.BaseURL+"/oauth/google/callback"),
		signer:     session.NewSigner(cfg.SessionSecret),
		log:        log,
		oauth:      oauthCfg,
		idVerifier: provider.Verifier(&oidc.Config{ClientID: cfg.BrokerOIDCClientID}),
	}, nil
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })

	// broker's own login
	mux.HandleFunc("GET /login", s.handleLogin)
	mux.HandleFunc("GET /auth/callback", s.handleAuthCallback)

	// connect flow (browser, needs broker session)
	mux.HandleFunc("GET /connect/{app_id}/start", s.handleConnectStart)
	mux.HandleFunc("GET /oauth/google/callback", s.handleGoogleCallback)

	// token vending (called by subapps, Bearer = subapp's Authentik access token)
	mux.HandleFunc("POST /token", s.handleToken)

	// management (browser, needs broker session)
	mux.HandleFunc("GET /connections", s.handleListConnections)
	mux.HandleFunc("DELETE /connections/{app_id}", s.handleDeleteConnection)

	return mux
}

// --- broker login ---

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	state := randToken()
	http.SetCookie(w, &http.Cookie{Name: "oidc_state", Value: state, Path: "/",
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: 300})
	http.Redirect(w, r, s.oauth.AuthCodeURL(state), http.StatusFound)
}

func (s *Server) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("oidc_state")
	if err != nil || r.URL.Query().Get("state") != c.Value {
		http.Error(w, "bad state", http.StatusBadRequest)
		return
	}
	oauth2Token, err := s.oauth.Exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		http.Error(w, "exchange failed", http.StatusBadGateway)
		return
	}
	rawID, _ := oauth2Token.Extra("id_token").(string)
	idToken, err := s.idVerifier.Verify(r.Context(), rawID)
	if err != nil {
		http.Error(w, "id_token invalid", http.StatusUnauthorized)
		return
	}
	var claims struct {
		Email string `json:"email"`
	}
	_ = idToken.Claims(&claims)
	s.signer.SetSession(w, session.User{Sub: idToken.Subject, Email: claims.Email}, 12*time.Hour)
	http.Redirect(w, r, "/connections", http.StatusFound)
}

// --- connect flow ---

func (s *Server) handleConnectStart(w http.ResponseWriter, r *http.Request) {
	user, err := s.signer.Session(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	appID := r.PathValue("app_id")
	app, err := s.store.AppByID(r.Context(), appID)
	if err != nil {
		http.Error(w, "unknown app", http.StatusNotFound)
		return
	}
	state := s.signer.SignState(session.State{
		AppID:     app.AppID,
		UserSub:   user.Sub,
		ReturnURL: r.URL.Query().Get("return"),
		Nonce:     randToken(),
	}, 10*time.Minute)
	url := s.google.AuthCodeURL(app.AllowedScopes, state, user.Email)
	http.Redirect(w, r, url, http.StatusFound)
}

func (s *Server) handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	st, err := s.signer.VerifyState(r.URL.Query().Get("state"))
	if err != nil {
		http.Error(w, "bad state", http.StatusBadRequest)
		return
	}
	tok, err := s.google.Exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		http.Error(w, "google exchange failed", http.StatusBadGateway)
		return
	}
	if tok.RefreshToken == "" {
		http.Error(w, "no refresh token returned (re-consent needed)", http.StatusBadGateway)
		return
	}
	enc, err := s.cipher.Encrypt(tok.RefreshToken)
	if err != nil {
		http.Error(w, "encrypt failed", http.StatusInternalServerError)
		return
	}
	app, _ := s.store.AppByID(r.Context(), st.AppID)
	grant := store.Grant{
		UserSub:          st.UserSub,
		AppID:            st.AppID,
		GrantedScopes:    app.AllowedScopes,
		RefreshTokenEnc:  enc,
		AccessTokenCache: tok.AccessToken,
	}
	if !tok.Expiry.IsZero() {
		grant.ExpiresAt = &tok.Expiry
	}
	if err := s.store.UpsertGrant(r.Context(), grant); err != nil {
		http.Error(w, "store failed", http.StatusInternalServerError)
		return
	}
	dest := st.ReturnURL
	if dest == "" {
		dest = "/connections"
	}
	http.Redirect(w, r, dest, http.StatusFound)
}

// --- token vending ---

type tokenResponse struct {
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
	Scopes      []string  `json:"scopes"`
}

func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	raw := bearer(r)
	if raw == "" {
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return
	}
	claims, err := s.verifier.Verify(r.Context(), raw)
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	// Map the token's client_id -> app. This is what stops App A vending for App B:
	// the app_id is derived from the *authenticated* token, never from the request body.
	app, err := s.store.AppByClientID(r.Context(), claims.ClientID)
	if err != nil {
		http.Error(w, "app not registered", http.StatusForbidden)
		return
	}
	grant, err := s.store.Grant(r.Context(), claims.Subject, app.AppID)
	if err != nil {
		http.Error(w, "not connected", http.StatusNotFound) // caller should prompt /connect
		return
	}
	// Serve cached token if still valid (30s skew).
	if grant.ExpiresAt != nil && time.Until(*grant.ExpiresAt) > 30*time.Second && grant.AccessTokenCache != "" {
		writeJSON(w, tokenResponse{AccessToken: grant.AccessTokenCache, ExpiresAt: *grant.ExpiresAt, Scopes: grant.GrantedScopes})
		return
	}
	refresh, err := s.cipher.Decrypt(grant.RefreshTokenEnc)
	if err != nil {
		http.Error(w, "decrypt failed", http.StatusInternalServerError)
		return
	}
	fresh, err := s.google.FreshAccessToken(r.Context(), refresh)
	if err != nil {
		http.Error(w, "refresh failed (re-consent may be needed)", http.StatusBadGateway)
		return
	}
	_ = s.store.UpdateAccessCache(r.Context(), claims.Subject, app.AppID, fresh.AccessToken, fresh.Expiry)
	// NOTE: never return fresh.RefreshToken.
	writeJSON(w, tokenResponse{AccessToken: fresh.AccessToken, ExpiresAt: fresh.Expiry, Scopes: grant.GrantedScopes})
}

// --- management ---

func (s *Server) handleListConnections(w http.ResponseWriter, r *http.Request) {
	user, err := s.signer.Session(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	grants, err := s.store.ListGrants(r.Context(), user.Sub)
	if err != nil {
		http.Error(w, "list failed", http.StatusInternalServerError)
		return
	}
	type item struct {
		AppID  string   `json:"app_id"`
		Scopes []string `json:"scopes"`
	}
	out := make([]item, 0, len(grants))
	for _, g := range grants {
		out = append(out, item{AppID: g.AppID, Scopes: g.GrantedScopes})
	}
	writeJSON(w, out)
}

func (s *Server) handleDeleteConnection(w http.ResponseWriter, r *http.Request) {
	user, err := s.signer.Session(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	appID := r.PathValue("app_id")
	grant, err := s.store.Grant(r.Context(), user.Sub, appID)
	if err == nil {
		if refresh, derr := s.cipher.Decrypt(grant.RefreshTokenEnc); derr == nil {
			_ = s.google.Revoke(r.Context(), refresh) // best-effort revoke at Google
		}
	}
	if err := s.store.DeleteGrant(r.Context(), user.Sub, appID); err != nil {
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ---

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && h[:7] == "Bearer " {
		return h[7:]
	}
	return ""
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func randToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

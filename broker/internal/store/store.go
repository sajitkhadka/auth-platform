// Package store is the Postgres persistence layer for the broker.
package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sajitkhadka/auth-platform/broker/migrations"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	pool *pgxpool.Pool
}

type App struct {
	AppID             string
	Name              string
	AllowedScopes     []string
	AuthentikClientID string
}

type Grant struct {
	UserSub          string
	AppID            string
	GrantedScopes    []string
	RefreshTokenEnc  string
	AccessTokenCache string
	ExpiresAt        *time.Time
}

func New(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

// Migrate applies the embedded .sql files in lexical order (idempotent DDL).
func (s *Store) Migrate(ctx context.Context) error {
	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names {
		b, err := migrations.Files.ReadFile(n)
		if err != nil {
			return err
		}
		if _, err := s.pool.Exec(ctx, string(b)); err != nil {
			return fmt.Errorf("apply %s: %w", n, err)
		}
	}
	return nil
}

// --- app_registry ---

func (s *Store) AppByID(ctx context.Context, appID string) (*App, error) {
	return s.scanApp(s.pool.QueryRow(ctx,
		`SELECT app_id, name, allowed_scopes, authentik_client_id FROM app_registry WHERE app_id=$1`, appID))
}

func (s *Store) AppByClientID(ctx context.Context, clientID string) (*App, error) {
	return s.scanApp(s.pool.QueryRow(ctx,
		`SELECT app_id, name, allowed_scopes, authentik_client_id FROM app_registry WHERE authentik_client_id=$1`, clientID))
}

func (s *Store) UpsertApp(ctx context.Context, a App) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO app_registry (app_id, name, allowed_scopes, authentik_client_id)
		 VALUES ($1,$2,$3,$4)
		 ON CONFLICT (app_id) DO UPDATE SET name=EXCLUDED.name,
		   allowed_scopes=EXCLUDED.allowed_scopes, authentik_client_id=EXCLUDED.authentik_client_id`,
		a.AppID, a.Name, a.AllowedScopes, a.AuthentikClientID)
	return err
}

func (s *Store) scanApp(row pgx.Row) (*App, error) {
	var a App
	err := row.Scan(&a.AppID, &a.Name, &a.AllowedScopes, &a.AuthentikClientID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// --- google_grant ---

func (s *Store) Grant(ctx context.Context, userSub, appID string) (*Grant, error) {
	var g Grant
	err := s.pool.QueryRow(ctx,
		`SELECT user_sub, app_id, granted_scopes, refresh_token_enc,
		        COALESCE(access_token_cache,''), expires_at
		 FROM google_grant WHERE user_sub=$1 AND app_id=$2`, userSub, appID).
		Scan(&g.UserSub, &g.AppID, &g.GrantedScopes, &g.RefreshTokenEnc, &g.AccessTokenCache, &g.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

func (s *Store) UpsertGrant(ctx context.Context, g Grant) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO google_grant (user_sub, app_id, granted_scopes, refresh_token_enc, access_token_cache, expires_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6, now())
		 ON CONFLICT (user_sub, app_id) DO UPDATE SET
		   granted_scopes=EXCLUDED.granted_scopes,
		   refresh_token_enc=EXCLUDED.refresh_token_enc,
		   access_token_cache=EXCLUDED.access_token_cache,
		   expires_at=EXCLUDED.expires_at,
		   updated_at=now()`,
		g.UserSub, g.AppID, g.GrantedScopes, g.RefreshTokenEnc, g.AccessTokenCache, g.ExpiresAt)
	return err
}

func (s *Store) UpdateAccessCache(ctx context.Context, userSub, appID, token string, exp time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE google_grant SET access_token_cache=$3, expires_at=$4, updated_at=now()
		 WHERE user_sub=$1 AND app_id=$2`, userSub, appID, token, exp)
	return err
}

func (s *Store) DeleteGrant(ctx context.Context, userSub, appID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM google_grant WHERE user_sub=$1 AND app_id=$2`, userSub, appID)
	return err
}

func (s *Store) ListGrants(ctx context.Context, userSub string) ([]Grant, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT user_sub, app_id, granted_scopes, refresh_token_enc,
		        COALESCE(access_token_cache,''), expires_at
		 FROM google_grant WHERE user_sub=$1 ORDER BY app_id`, userSub)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Grant
	for rows.Next() {
		var g Grant
		if err := rows.Scan(&g.UserSub, &g.AppID, &g.GrantedScopes, &g.RefreshTokenEnc, &g.AccessTokenCache, &g.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

-- auth-platform :: Postgres provisioning
-- =====================================================================
-- Reuses the existing host Postgres instances on 192.168.0.120:
--   prod -> :5432    dev -> :5433
-- Run this as the postgres superuser against EACH instance. The prod
-- instance gets the non-suffixed roles/DBs; the dev instance gets the
-- *_dev roles/DBs. (The block is written so it is safe to run on either;
-- create only the pair you need per instance, or run the whole file on
-- both — CREATE ... guards make it idempotent-ish.)
--
-- Passwords below are PLACEHOLDERS. Replace before running, then feed the
-- resulting connection strings into the sealed secrets (never commit them).
--
-- Usage (from a machine with psql, or via a throwaway pod — see README):
--   PROD: psql "postgres://postgres:***@192.168.0.120:5432/postgres" -f provision.sql
--   DEV : psql "postgres://postgres:***@192.168.0.120:5433/provision.sql"  (use the *_dev vars)
-- =====================================================================

-- ---------- PROD (run against :5432) ----------
-- Authentik
CREATE ROLE authentik LOGIN PASSWORD 'REPLACE_AUTHENTIK_DB_PASSWORD';
CREATE DATABASE authentik OWNER authentik;
-- Broker (Google Connections)
CREATE ROLE connect LOGIN PASSWORD 'REPLACE_CONNECT_DB_PASSWORD';
CREATE DATABASE connect OWNER connect;

-- ---------- DEV (run against :5433) ----------
-- Authentik (dev)
-- CREATE ROLE authentik_dev LOGIN PASSWORD 'REPLACE_AUTHENTIK_DEV_DB_PASSWORD';
-- CREATE DATABASE authentik_dev OWNER authentik_dev;
-- Broker (dev)
-- CREATE ROLE connect_dev LOGIN PASSWORD 'REPLACE_CONNECT_DEV_DB_PASSWORD';
-- CREATE DATABASE connect_dev OWNER connect_dev;

-- Resulting DATABASE_URLs (sslmode=disable, LAN-only — matches sync-todo pattern):
--   authentik  prod: postgres://authentik:***@192.168.0.120:5432/authentik?sslmode=disable
--   connect    prod: postgres://connect:***@192.168.0.120:5432/connect?sslmode=disable
--   authentik  dev : postgres://authentik_dev:***@192.168.0.120:5433/authentik_dev?sslmode=disable
--   connect    dev : postgres://connect_dev:***@192.168.0.120:5433/connect_dev?sslmode=disable

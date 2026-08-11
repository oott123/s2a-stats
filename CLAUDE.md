# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`s2astats` is a standalone Go service that connects **read-only** to an existing sub2api instance's PostgreSQL database, runs `SELECT`-only queries, and exposes a small set of **public read-only** statistics endpoints (authenticated by a single public token). It never goes through sub2api's HTTP API and holds no admin key. sub2api's source is vendored at `.references/sub2api` purely as a schema/field reference (gitignored).

## Commands

- Run the server: `mise run server` (mise injects env from `.env.default` then `.env`). Equivalent to `go run ./cmd/app/main.go`.
- Build / vet: `go build ./...` && `go vet ./...`
- Test all: `go test ./...`
- Single test: `go test ./internal/stats -run TestBillingCycle -v`
- Always `gofmt -w` files you touch; a linter auto-formats on save, so don't fight it.

Required env to actually boot (see `.env.default` for all keys): `S2A_SUB2API_DSN` (read-only Postgres DSN) and `S2A_PUBLIC_TOKEN`. Missing either → process exits at startup by design.

## Architecture

Request flow: `httpapi` (auth + routing + JSON) → `stats.Service` (time-window/billing math, DTO assembly, caching) → `store` (read-only pgx queries) → sub2api Postgres.

- `cmd/app/main.go` — wiring + graceful shutdown. `import _ "time/tzdata"` embeds the tz database so containers without zoneinfo still resolve `Asia/Singapore`.
- `internal/config` — env-only config; strict validation, fail-fast on missing/invalid.
- `internal/store` — the **only** package that touches the DB. Three queries: `AnthropicAccountWindows` / `AnthropicAccountWindow` (shared SQL predicate + column list, single-account variant is the `{id}` existence check) and `UserStandardCost` (reqs 2 & 3, shared). pgxpool.
- `internal/stats` — business logic + DTOs. `Service` takes a `dataStore` interface (not the concrete store) so it's unit-testable; injects `now func() time.Time` for the same reason. Per-endpoint TTL cache keyed by endpoint+normalized params.
- `internal/httpapi` — Go 1.22+ `ServeMux` method+path patterns, no web framework. `auth` middleware accepts `Authorization: Bearer <token>` or `?token=`, compared with `subtle.ConstantTimeCompare`. `/healthz` is unauthenticated.
- `internal/cache` — generic map+RWMutex TTL cache, lazy expiry on read.

### Domain rules that aren't obvious from the code

- **Standard cost** = `SUM(usage_logs.total_cost)` (raw billed USD, no group/account multipliers). Aggregation dimension is **user + account**; sub2api's Group entity is deliberately ignored.
- **Account selection** filters `platform = 'anthropic' AND type IN ('oauth','setup-token') AND deleted_at IS NULL` — the single account set shared by all three endpoints (`GET /v1/accounts`, `{id}/window-usage`, `{id}/monthly-usage`). Only Anthropic OAuth / Setup Token accounts — apikey/upstream/bedrock/service_account 等类型被排除. `accounts` is soft-deleted (sub2api's `SoftDeleteMixin`: `deleted_at IS NULL` = live). The two `{id}` endpoints return `404 account not found` for ids outside this set, identical for "does not exist" and "fails the filter" so the API cannot probe non-Anthropic accounts.
- **5h / 7d windows (req 2)** align to the account's Anthropic reset instants from passive sampling: `window_start = reset - 5h/7d`, `window_end = now`. The 5h reset is the `session_window_end` column; the 7d reset lives in `extra` JSONB (`passive_usage_7d_reset`, unix seconds). A window with no reset → `available:false`.
- **Billing cycle (req 3)** is `[the 10th 00:00, next month's 10th 00:00)` evaluated in `Asia/Singapore` (UTC+8, no DST), via `BillingCycle` in `stats/windows.go`.
- `usage_logs` is append-only (no soft delete). Its `LEFT JOIN users` is for display only — historical cost stays attributed even to soft-deleted users, so do **not** add a `deleted_at` filter there.
- **Privacy:** responses expose only account `id/name/status`, window utilization/resets, username, and standard cost. Username falls back to `user-<id>` when empty. Never expose email, tokens, concurrency, or multipliers.

### sub2api schema notes (from `.references/sub2api`)

Tables: `accounts`, `usage_logs`, `users`. Some Ent schema comments are stale — e.g. `account.go` says platform `"claude"` (actual constant is `"anthropic"`) and type `"api_key"` (actual is `"apikey"`). Trust `internal/domain/constants.go` over the comments. Verify field/column names against the vendored source before relying on them.

## Working Conventions

- **No unsolicited compatibility layers.** When planning or making decisions, do not introduce compatibility shims, fallbacks, deprecated-path branches, or any code whose purpose is to preserve old behavior — unless the user explicitly asks for it. Prefer clean replacements that update all call sites. If you genuinely believe a compatibility layer is unavoidable or strongly warranted, **stop and ask the user before writing it**.
- **Fail fast on input; do not be lenient.** Do not add forgiving normalization of user/API input — no silent trimming of whitespace, no case-folding, no coercing empty strings to defaults, no "did you mean" guessing, no accepting near-miss formats. Validate strictly and reject invalid input with a clear error. Only relax this when the user explicitly asks for it.

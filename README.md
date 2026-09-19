# fakerforge CLI

Pull FakerForge synthetic data directly into your local/private database — without ever sending DB credentials to the backend.

Two connections, never bridged:

1. **CLI → FakerForge API** (outbound HTTPS) — auth, schema upload, streaming rows.
2. **CLI → your local DB** (Postgres/MySQL) — credentials stay in your OS keychain.

## Install

```bash
go install github.com/kwasii1/fakerforge-db-agent@latest
# or from source:
git clone https://github.com/kwasii1/fakerforge-db-agent.git
cd fakerforge-db-agent
go build -o fakerforge .
```

Requires Go 1.27+.

## Quick start

```bash
# Point at your backend (default http://localhost:8000 for dev,
# https://fakerforge.com in production via env):
export FAKERFORGE_API_URL=http://localhost:8000

fakerforge login                 # validates GET /api/me, saves key in OS keychain
fakerforge whoami

fakerforge connect                         # interactive: prompts for name, driver, host, port, database, user, password
# or fully flagged (scriptable):
fakerforge connect --name local --driver postgres \
  --host localhost --port 5432 --database myapp --user postgres
# password via --password, FAKERFORGE_DB_PASSWORD, or interactive prompt
fakerforge connect --list

fakerforge schema pull --connection local [--tables users,orders] [--rows 100]
# introspects + builds parsed tables locally (no server parsing, no AI),
# uploads shape only → generates → polls to ready with a live progress bar
# (overall + per-table; TTY only, single-line fallback when piped; --no-progress for CI)
# (or --async to return early)
# re-pulls are idempotent via fingerprint (--force / --regenerate to redo)

fakerforge schemas list --format table
fakerforge schemas show <schema-id>            # tables with rows + status
fakerforge schemas show <schema-id> --table users  # columns, status, sample

fakerforge push --schema <schema-id> --connection local --table users --dry-run
fakerforge push --schema <schema-id> --connection local --table users
# no --table: TRUNCATE + INSERT every ready table, parents-first (confirmed);
# --append to insert without truncating

fakerforge status   # API auth + per-connection ping + CLI version check
```

## Commands

| Command | Notes |
|---|---|
| `login [--api-key]` | flag > `FAKERFORGE_API_KEY` > prompt; fails loudly on bad key |
| `logout`, `whoami` | keychain delete / `GET /api/me` |
| `connect` | `--list`, `--remove NAME`, `--default NAME`; `Ping()` before save; first conn becomes default |
| `schema pull` | multi-table introspect → CLI-built parsed tables + DDL → `POST /api/schemas` → generate → poll `…/progress` to ready with live overall + per-table bar; `--tables`, `--rows` (def. 100), `--force`, `--regenerate`, `--async`, `--no-progress`, `--timeout`, `--interval` |
| `schemas list` | `GET /api/schemas[?table=]` → `SCHEMA ID NAME TABLES CREATED`; `--format json` for CI |
| `schemas show` | `GET /api/schemas/{id}[?table=]` → per-table rows + status; with `--table`, columns/rules + sample |
| `push` | `--table` for single-table append; otherwise TRUNCATE + INSERT all ready tables parents-first (always confirmed unless `--yes`); `--append` skips truncate; `--dry-run`; per-table subset checks; abort on first failure with tallies |
| `status` | exit non-zero if auth or connections broken |

## Security

- API key: OS keychain (`fakerforge-cli`/`api_key`), fallback `~/.fakerforge/.secrets` (0600) on headless machines.
- DB passwords: keychain `conn:{name}`, never in `~/.fakerforge/connections.yaml` (metadata only).
- `push` uses parameterized queries only, batch rollback on failure, reports succeeded-row count.

## Backend contract (Laravel)

The CLI expects (stub-tested; implement in `dbseeder` if missing):

```
GET  /api/me
POST /api/schemas
GET  /api/schemas?status=&table=
GET  /api/schemas/{id}?table=
GET  /api/schemas/{id}/{table}/stream   (JSONL, chunked)
GET  /api/cli/latest
```

All with `Authorization: Bearer {api_key}`. Current `dbseeder` has `GET /api/{schemaId}/{table}` — generalize that handler into `/stream`.

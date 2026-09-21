# fakerforge CLI

[![Release](https://img.shields.io/github/v/release/kwasii1/fakerforge-db-agent)](https://github.com/kwasii1/fakerforge-db-agent/releases)
[![CI](https://github.com/kwasii1/fakerforge-db-agent/actions/workflows/ci.yml/badge.svg)](https://github.com/kwasii1/fakerforge-db-agent/actions/workflows/ci.yml)

Pull FakerForge synthetic data directly into your local/private database — without ever sending DB credentials to the backend.

Two connections, never bridged:

1. **CLI → FakerForge API** (outbound HTTPS) — auth, schema upload, streaming rows.
2. **CLI → your local DB** (Postgres/MySQL) — credentials stay in your OS keychain.

Full guide: [fakerforge.com/docs/fakerforge-cli](https://fakerforge.com/docs/fakerforge-cli)

## Install

Pick one:

```bash
# 1. Install script (Linux/macOS) — latest release, checksum-verified
curl -sSL https://raw.githubusercontent.com/kwasii1/fakerforge-db-agent/main/install.sh | sh
# pin a version: ... | sh -s -- v0.2.0
# custom dir:    ... | INSTALL_DIR=$HOME/bin sh

# 2. Homebrew (macOS/Linux)
brew tap kwasii1/fakerforge
brew install fakerforge

# 3. Go toolchain (any OS, requires Go 1.27+)
go install github.com/kwasii1/fakerforge-db-agent@latest

# 4. Manual download — pick your archive from the
# Releases page, verify, unpack:
# https://github.com/kwasii1/fakerforge-db-agent/releases
sha256sum -c checksums.txt   # must mention your archive
tar -xzf fakerforge_linux_amd64.tar.gz
sudo mv fakerforge /usr/local/bin/
```

Supported platforms: `linux` / `darwin` / `windows` × `amd64` / `arm64` (Windows: use the `.zip` from the Releases page).

Update: re-run your install method, then `fakerforge status` to confirm the
installed version matches the latest release.

## Quick start

The CLI talks to `https://fakerforge.com` by default — no configuration needed.

### 1. Log in

```bash
fakerforge login
fakerforge whoami
```

The key is validated immediately and stored in your OS keychain.

### 2. Register your database

```bash
fakerforge connect --name local --driver postgres \
  --host localhost --port 5432 --database myapp --user postgres
```

Run `fakerforge connect` with no flags for interactive prompts. The password comes from `--password`, `FAKERFORGE_DB_PASSWORD`, or a prompt.

### 3. Generate data

```bash
fakerforge schema pull --connection local --tables users,orders --rows 100
```

Your database is introspected locally and only the schema shape is uploaded. Re-pulls are idempotent; watch progress live, or pass `--async` to return early.

### 4. Inspect and push

```bash
fakerforge schemas show <schema-id>
fakerforge push --schema <schema-id> --connection local --table users --dry-run
fakerforge push --schema <schema-id> --connection local --table users
```

Omit `--table` (with `--yes`) to `TRUNCATE + INSERT` every ready table, parents-first.

### 5. Health check

```bash
fakerforge status
```

## Commands

| Command | Flags |
|---|---|
| `login` | `--api-key` (flag > `FAKERFORGE_API_KEY` > prompt); `--api-url`; fails loudly on bad key |
| `logout`, `whoami` | keychain delete / `GET /api/me` |
| `connect` | `--name`, `--driver postgres\|mysql`, `--host`, `--port`, `--database`, `--user`, `--password`; `--list`, `--remove NAME`, `--default NAME`; `Ping()` before save; first conn becomes default |
| `schema pull` | `--connection` (default: default connection), `--tables A,B` (`--table T` deprecated single-item alias), `--rows N` (default 100, capped by plan), `--name`, `--force`, `--regenerate`, `--async`, `--timeout` (default 20m), `--interval` (default 3s), `--no-progress` |
| `schemas list` | `--table TABLE` filter; `--format table\|json` |
| `schemas show` | `SCHEMA_ID`; `--table TABLE` for columns/rules + sample |
| `push` | `--schema ID`, `--connection NAME`, `--table TABLE` (single-table append), `--batch-size N` (default 500, 1–5000), `--dry-run`, `--yes`, `--append`; TRUNCATE + INSERT all ready tables parents-first when `--table` is omitted |
| `status` | exit non-zero if auth or connections broken |
| `version`, `--version`, `-V` | print the CLI version (release builds stamp the git tag) |

Global: `--no-color` (any position), `--api-url` / `--api-key` overrides on most commands.

Environment:

| Variable | Purpose |
|---|---|
| `FAKERFORGE_API_URL` | API base URL override (default `https://fakerforge.com`; maintainers use `http://127.0.0.1:8001` locally) |
| `FAKERFORGE_API_KEY` | API key (overrides keychain) |
| `FAKERFORGE_DB_PASSWORD` | DB password for scripting |
| `FAKERFORGE_HOME` | config dir override (default `~/.fakerforge`) |

## Security

- API key: OS keychain (`fakerforge-cli`/`api_key`), fallback `~/.fakerforge/.secrets` (0600) on headless machines.
- DB passwords: keychain `conn:{name}`, never in `~/.fakerforge/connections.yaml` (metadata only).
- `push` uses parameterized queries only, batch rollback on failure, reports succeeded-row count.
- Every generated row is validated against the target table's constraints (integer ranges, string lengths, enum values, NOT NULL, uniqueness) before insert — bad data aborts the push with the exact row, column, and value instead of a raw database error.

## Troubleshooting

- `connection failed` / ping errors: check host, port, and that the DB accepts TCP from your machine; passwords come from `--password`, `FAKERFORGE_DB_PASSWORD`, or the prompt (never a bare positional arg).
- No credential prompt on a server: set `FAKERFORGE_API_KEY` / `FAKERFORGE_DB_PASSWORD` env vars (keychain needs an interactive session).
- `push` aborted with a value-rejected error: the message names the row/column/value — regenerate the schema (`schema pull --regenerate`) or fix the offending data.
- Target host looks like production (`prod`/`production` in the hostname): `push` demands an extra confirmation.
- `status` exits non-zero: read the per-connection report — it tells you whether auth, a connection, or the CLI version is the problem.

## Backend contract (Laravel)

The CLI expects (served by `dbseeder`):

```
GET  /api/me
POST /api/schemas
GET  /api/schemas?status=&table=
GET  /api/schemas/{id}?table=
GET  /api/schemas/{id}/{table}/stream   (JSONL, chunked)
GET  /api/cli/latest
```

All with `Authorization: Bearer {api_key}`.

## License

MIT — see [LICENSE](LICENSE).

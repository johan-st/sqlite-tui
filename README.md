# sqlite-tui

A TUI and CLI database studio for SQLite, accessible locally or via SSH.

## Features

- **SSH Access**: Connect remotely with public key authentication
- **Interactive TUI**: Full terminal UI for browsing and editing databases
- **CLI Commands**: Scriptable commands for automation and piping
- **Database Discovery**: Glob patterns, directories, real-time file watching
- **Access Control**: Per-user permissions (none/read-only/read-write/admin)
- **Anonymous Access**: Optional keyless connections with generated names

## Installation

```bash
go install github.com/johan-st/sqlite-tui/cmd/sqlite-tui@latest
```

Or build from source:

```bash
make build
```

## Usage

### Local Mode (single user, admin)

Open a database, directory, or glob pattern directly:

```bash
# TUI mode (interactive)
sqlite-tui mydb.db
sqlite-tui ./databases/
sqlite-tui "./data/*.db"

# CLI mode (run command and exit)
sqlite-tui mydb.db ls
sqlite-tui mydb.db tables mydb
sqlite-tui mydb.db query mydb "SELECT * FROM users"
sqlite-tui mydb.db export mydb users --format=csv > users.csv
```

You are automatically admin with full read-write access. No config file needed.

### SSH Server Mode (multi-user)

Start the SSH server with a config file:

```bash
sqlite-tui -ssh -config config.yaml
```

Users connect via SSH with public key authentication:

```bash
# Interactive TUI
ssh -t user@host -p 2222

# CLI commands
ssh user@host -p 2222 ls
ssh user@host -p 2222 query mydb "SELECT * FROM users"
```

## CLI Commands

Connect via SSH and run commands:

```bash
ssh host[:port] <command> [args] [options]
```

### Database Commands

| Command | Usage | Description |
|---------|-------|-------------|
| `ls` / `list` | `ls [--format=json]` | List accessible databases |
| `info` | `info <database>` | Show database info |
| `tables` | `tables <database>` | List tables in database |
| `schema` | `schema <database> <table>` | Show table schema |

### Query Commands

| Command | Usage | Description |
|---------|-------|-------------|
| `query` | `query <database> "<sql>"` | Execute raw SQL |
| `select` | `select <database> <table> [--where=...] [--limit=N]` | Browse table data |
| `count` | `count <database> <table> [--where=...]` | Count rows |

### Data Commands (requires write access)

| Command | Usage | Description |
|---------|-------|-------------|
| `insert` | `insert <database> <table> --json='{"col":"val"}'` | Insert row |
| `update` | `update <database> <table> --where="..." --set='{"col":"val"}'` | Update rows |
| `delete` | `delete <database> <table> --where="..." --confirm` | Delete rows |

### Export Commands

| Command | Usage | Description |
|---------|-------|-------------|
| `export` | `export <database> <table> [--format=csv\|json]` | Export table data to stdout |
| `download` | `download <database>` | Stream raw .db file to stdout |

### Schema Commands (requires write access)

| Command | Usage | Description |
|---------|-------|-------------|
| `create-table` | `create-table <database> <table> --columns="id:int:pk,name:text"` | Create new table |
| `add-column` | `add-column <database> <table> <column> <type> [--default=...]` | Add column |
| `drop-table` | `drop-table <database> <table> --confirm` | Drop table |

### Admin Commands (requires admin access)

| Command | Usage | Description |
|---------|-------|-------------|
| `sessions` | `sessions` | List active sessions |
| `history` | `history` | View query history |
| `audit` | `audit` | View audit log |
| `reload-config` | `reload-config` | Reload config file |

### Utility Commands

| Command | Usage | Description |
|---------|-------|-------------|
| `whoami` | `whoami` | Show current user info |
| `help` | `help [command]` | Show help |
| `version` | `version` | Show version |

### Common Options

- `--format=json` - JSON output
- `--format=csv` - CSV output
- `--limit=N` - Limit rows
- `--offset=N` - Skip N rows

## Configuration

See [`config.example.yaml`](config.example.yaml) for a complete example.

```yaml
server:
  ssh:
    enabled: true
    listen: ":2222"
    host_key_path: ".sqlite-tui/host_key"
  local:
    enabled: true

databases:
  - path: "./*.db"
    description: "Local databases"

anonymous_access: "none"
allow_keyless: false

users:
  - name: admin
    admin: true
    public_keys:
      - "ssh-ed25519 AAAAC3..."
```

## License

MIT


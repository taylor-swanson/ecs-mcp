# ecs-mcp

`ecs-mcp` is a Model Context Protocol (MCP) server that enables LLMs to query
metadata about Elastic Common Schema (ECS).

## Features

- Loads ECS field definitions from the official **ecs_flat.yml** file
- **Fetches from GitHub by default** — no local ECS repo required; downloads
  `ecs_flat.yml` from the [elastic/ecs](https://github.com/elastic/ecs) repository
- Optional local file: use a local `ecs_flat.yml` path instead of GitHub
- Builds a queryable SQLite database with ECS field metadata
- Exposes read-only database access to LLMs through the Model Context Protocol
- Enables AI assistants to answer detailed questions about ECS fields

## Usage

### Default: fetch ECS from GitHub

With no extra arguments, the server fetches the ECS definition from GitHub
(`https://raw.githubusercontent.com/elastic/ecs/main/generated/ecs/ecs_flat.yml`)
and uses it to build the database. No local ECS repo or file is required.

#### With `go run`

```json
{
  "mcpServers": {
    "ecs": {
      "command": "go",
      "args": [
        "run",
        "github.com/taylor-swanson/ecs-mcp@main"
      ]
    }
  }
}
```

#### Local install

Install the binary:

```bash
go install github.com/taylor-swanson/ecs-mcp
```

Then use the binary path (e.g. from `which ecs-mcp`):

```json
{
  "mcpServers": {
    "ecs": {
      "command": "/Users/<USERNAME>/go/bin/ecs-mcp"
    }
  }
}
```

### Optional: use a local ECS file

To use a local `ecs_flat.yml` instead of fetching from GitHub:

```json
{
  "mcpServers": {
    "ecs": {
      "command": "/Users/<USERNAME>/go/bin/ecs-mcp",
      "args": ["-ecs-file", "/path/to/ecs/generated/ecs/ecs_flat.yml"]
    }
  }
}
```

### Optional: choose a different Git ref (branch or tag)

When fetching from GitHub, you can pin to a branch or tag with `-git-ref`:

```json
{
  "mcpServers": {
    "ecs": {
      "command": "/Users/<USERNAME>/go/bin/ecs-mcp",
      "args": ["-git-ref", "v9.3.0"]
    }
  }
}
```

## Command-line arguments

| Argument     | Description | Environment variable |
|-------------|-------------|----------------------|
| `-db`       | Path to the SQLite database file (default: `ecs-mcp.db`) | `ECS_MCP_DB_FILE` |
| `-ecs-file` | Path to a local **ecs_flat.yml** file. When omitted, the definition is fetched from GitHub. | `ECS_MCP_ECS_FILE` |
| `-git-ref`  | When using GitHub, the git ref to use, e.g. `main` or `v9.3.0` (default: `main`) | `ECS_MCP_GIT_REF` |
| `-listen`   | Listen for HTTP on the given address instead of stdio | `ECS_MCP_LISTEN` |
| `-cert`     | Path to TLS certificate file (default: `cert.pem`), used with `-listen` | `ECS_MCP_CERT_FILE` |
| `-key`      | Path to TLS private key file (default: `key.pem`), used with `-listen` | `ECS_MCP_KEY_FILE` |
| `-insecure` | Disable TLS when using `-listen` | `ECS_MCP_INSECURE` |
| `-version`  | Print version information and exit | — |
| `-debug`    | Enable debug logging | `ECS_MCP_DEBUG` |

## Database schema

The SQLite database contains information about ECS fields from the flat definition, including:

- **Fields**: Metadata about each field (name, type, description)

## License

This project is licensed under the Apache License 2.0 — see the LICENSE.txt file for details.

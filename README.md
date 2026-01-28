# ecs-mcp

`ecs-mcp` is a Model Context Protocol (MCP) server that enables LLMs to query 
metadata about Elastic Common Schema (ECS).

## Features

- Scans and indexes all ECS fields from your local `elastic/ecs` repository
- Creates a queryable SQLite database with ECS field metadata
- Exposes readonly database access to LLMs through the Model Context Protocol
- Enables AI assistants to answer detailed questions about ECS fields

## Usage

### With `go run`

```json
{
  "mcpServers": {
    "ecs": {
      "command": "go",
      "args": [
        "run",
        "github.com/taylor-swanson/ecs-mcp@main",
        "-dir"
        "/Users/<USERNAME>/code/elastic/ecs"
      ]
    }
  }
}
```

### Local Install

Install the binary with

`go install github.com/taylor-swanson/ecs-mcp`

then determine the path using `which ecs-mcp`.

```
{
  "mcpServers": {
    "ecs": {
      "command": "/Users/<USERNAME>/go/bin/ecs-mcp"
    }
  }
}
```

### Required Arguments

- `-dir`: **Required**. Path to your local checkout of the [elastic/ecs](https://github.com/elastic/ecs) repository.

### Optional Arguments

- `-db`: Path to database file. Default: ecs-mcp.db
- `-listen`: Listen for HTTP at the specified address, instead of using stdin/stdout
- `-pretty`: Enable human-readable text logging
- `-debug`: Enable debug logging

## Database Schema

The SQLite database contains information about ECS files including:

- **Fields**: Metadata about each field (name, type, description)

## License

This project is licensed under the Apache License 2.0 - see the LICENSE.txt file for details.

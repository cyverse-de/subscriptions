# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build Commands

```bash
# Build the subscriptions service
make

# Clean build artifacts
make clean

# Run the tests
go test ./...

# Download Go dependencies
go mod download

# Run golangci-lint (requires golangci-lint to be installed)
golangci-lint run
```

## Architecture Overview

This is an HTTP microservice implementing subscription management functionality, designed to replace the legacy
QMS service. The service handles user subscriptions, quotas, usage tracking, and resource management through a
JSON-over-HTTP API built on Echo.

### Core Components

1. **main.go** - Entry point that:
   - Loads the layered configuration
   - Sets up PostgreSQL database connection with OpenTelemetry instrumentation
   - Starts the HTTP server

2. **app/** - Application layer containing business logic handlers:
   - `app.go` - Main application struct, route registration, and the user-update handlers
   - `users.go` - User management handlers
   - `plans.go` - Subscription plan management
   - `quotas.go` - Quota management and validation
   - `usages.go` - Resource usage tracking
   - `addons.go` - Subscription addon management
   - `overages.go` - Overage checking and reporting
   - `summary.go` - User subscription summary generation

3. **db/** - Database layer with PostgreSQL operations:
   - `db.go` - Database connection and transaction management
   - `types.go` - Database model types and structures
   - `tables/` - SQL table definitions using goqu query builder
   - Resource-specific files matching app/ structure

### HTTP API

The routes are registered in `app/app.go` and cover:
- User updates and usage tracking
- Subscription and plan management
- Quota operations
- Addon management
- Overage checking

Request and response bodies are plain JSON using the types from github.com/cyverse-de/p (whose struct tags are the wire contract).

## Configuration

The service uses a layered configuration approach:
1. Configuration file: `/etc/cyverse/de/configs/service.yml` (or specify with `--config`)
2. Dotenv file: `/etc/cyverse/de/env/service.env` (or specify with `--dotenv-path`)
3. Environment variables with `QMS_` prefix

Key configuration settings:
- `QMS_DATABASE_URI` - PostgreSQL connection string
- `QMS_USERNAME_SUFFIX` - User domain suffix (e.g., @iplantcollaborative.org)

## Local Development

```bash
# Create local dotenv file with configuration
echo 'QMS_USERNAME_SUFFIX=@iplantcollaborative.org
QMS_DATABASE_URI=postgresql://de@localhost/qms?sslmode=disable' > dotenv

# Run the service (listens on port 60000 by default; use --port to change it)
./subscriptions --dotenv-path dotenv
```

## Testing the API

```bash
# Get a user summary
curl -s http://localhost:60000/summary/sarahr | jq
```

## Dependencies

- Echo for HTTP routing
- PostgreSQL for data persistence
- goqu for SQL query building
- OpenTelemetry for observability
- Message types from github.com/cyverse-de/p, serialized as plain JSON (wire-compatible with the old protojson format)

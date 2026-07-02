# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build Commands

```bash
# Build the main subscriptions service
make

# Build both subscriptions and dev-test programs
make all

# Clean build artifacts
make clean

# Download Go dependencies
go mod download

# Run golangci-lint (requires golangci-lint to be installed)
golangci-lint run
```

## Architecture Overview

This is a NATS-centric microservice implementing subscription management functionality, designed to replace the legacy
QMS service. The service handles user subscriptions, quotas, usage tracking, and resource management through NATS
messaging.

### Core Components

1. **main.go** - Entry point that:
   - Configures NATS connection with TLS/credentials support
   - Sets up PostgreSQL database connection with OpenTelemetry instrumentation
   - Registers NATS message handlers for various QMS subjects
   - Starts HTTP server for health checks and metrics

2. **app/** - Application layer containing business logic handlers:
   - `app.go` - Main application struct and initialization
   - `users.go` - User management handlers
   - `plans.go` - Subscription plan management
   - `quotas.go` - Quota management and validation
   - `usages.go` - Resource usage tracking
   - `addons.go` - Subscription addon management
   - `overages.go` - Overage checking and reporting
   - `summary.go` - User subscription summary generation
   - `userPlans.go` - User-plan associations

3. **db/** - Database layer with PostgreSQL operations:
   - `db.go` - Database connection and transaction management
   - `types.go` - Database model types and structures
   - `tables/` - SQL table definitions using goqu query builder
   - Resource-specific files matching app/ structure

4. **natscl/** - NATS client wrapper for connection management

### NATS Message Handlers

The service subscribes to these NATS subjects (defined in go-mod/subjects/qms):
- User updates and usage tracking
- Subscription and plan management
- Quota operations
- Addon management
- Overage checking

All handlers follow a request/response pattern; messages are plain JSON using the types from github.com/cyverse-de/p (whose struct tags are the wire contract).

## Configuration

The service uses a layered configuration approach:
1. Configuration file: `/etc/cyverse/de/configs/service.yml` (or specify with `--config`)
2. Dotenv file: `/etc/cyverse/de/env/service.env` (or specify with `--dotenv-path`)
3. Environment variables with `QMS_` prefix

Key configuration settings:
- `QMS_DATABASE_URI` - PostgreSQL connection string
- `QMS_NATS_CLUSTER` - NATS cluster URLs
- `QMS_USERNAME_SUFFIX` - User domain suffix (e.g., @iplantcollaborative.org)

## Local Development

For local development without TLS/credentials:
```bash
# Create local dotenv file with configuration
echo 'QMS_USERNAME_SUFFIX=@iplantcollaborative.org
QMS_DATABASE_URI=postgresql://de@localhost/qms?sslmode=disable
QMS_NATS_CLUSTER=nats://localhost:4222' > dotenv

# Run the service
./subscriptions --no-tls --no-creds --dotenv-path dotenv
```

## Testing with NATS

```bash
# Subscribe to responses
nats sub 'foo.bar'

# Send a request (example: get user summary)
nats pub --reply=foo.bar cyverse.qms.user.summary.get '{"username":"sarahr"}'
```

## Dependencies

- NATS for messaging
- PostgreSQL for data persistence
- goqu for SQL query building
- OpenTelemetry for observability
- Message types from github.com/cyverse-de/p, serialized as plain JSON (wire-compatible with the old protojson format)

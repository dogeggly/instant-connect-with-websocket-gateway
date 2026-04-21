# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a distributed instant messaging system with WebSocket gateway architecture. The system uses a microservices approach with three main components:

1. **websocket-gateway** (Go): Handles WebSocket connections, message routing, and real-time push notifications
2. **instant-messaging** (Java/Spring Boot): REST API for user management, contacts, groups, and message handling
3. **store-consumer** (Java/Spring Boot): RabbitMQ consumer that writes messages to user timeline tables asynchronously

## Key Technologies

- **Go 1.26**: websocket-gateway uses Gorilla WebSocket, Redis, RabbitMQ
- **Java 21**: Business logic uses Spring Boot 3.5.9, MyBatis-Plus
- **PostgreSQL**: Primary database with pg_trgm extension for fuzzy search
- **Redis**: Connection routing, online status, distributed locks
- **RabbitMQ**: Message queue for gateway communication and async timeline processing
- **Protocol Buffers**: Message serialization between services
- **MinIO/S3**: File storage for large file uploads with resumable transfers

## Common Commands

### Running the Gateway (Go)
```bash
cd websocket-gateway
go run .
```

### Running the Java Services
```bash
cd instant-messaging  # or store-consumer
mvn spring-boot:run
```

### Protocol Buffer Compilation
```bash
cd protobuf
protoc --go_out=../websocket-gateway/pb --go_opt=paths=source_relative \
  --java_out=../instant-messaging/src/main/java --java_opt=paths=source_relative \
  mq_payload.proto
```

## Architecture Highlights

### Message Flow
1. Client sends message via REST API to `instant-messaging`
2. Service saves to `messages` table (global message log)
3. Service saves to `timeline_task` table (async processing queue)
4. Service queries Redis for user routing and publishes to RabbitMQ
5. `websocket-gateway` consumes from RabbitMQ and pushes to connected clients
6. `store-consumer` consumes from `timeline_task` and writes to user `timeline` tables

### Message Routing Strategy
- **Direct messages**: Use RabbitMQ direct exchange with routing key = gateway node ID
- **Group messages**: Use RabbitMQ fanout exchange, each gateway matches against its local connections
- **Offline messages**: Users pull from `timeline` table using `seq_id` cursor (not `msg_id`)

### Critical Design Decisions

**Why `seq_id` instead of `msg_id` for cursors?**
Snowflake IDs can arrive out of order due to network timing. Redis `incr` guarantees monotonically increasing sequence IDs per user, preventing message gaps when pulling offline messages.

**Why separate `timeline_task` and `timeline` tables?**
Decouples message ingestion from timeline writing via MQ. Allows:
- Fast message ACK to client (write to `messages` + `timeline_task` only)
- Async processing via `store-consumer` with retry mechanism
- Sharding: `messages` by hash, `timeline` by `owner_id`

**Connection state management:**
- Each gateway maintains in-memory connection map (`userId:deviceId` -> client)
- Redis stores routing info with CAS using connection ID (snowflake)
- Old connections are automatically cleaned when replaced (same user+device reconnection)

### File Upload with Resumable Transfer
- Uses state machine: 0=uninitialized, 1=initializing, 2=uploading, 3=complete
- Redis CAS prevents duplicate initialization
- MinIO multipart upload for chunked uploads
- File hash-based deduplication for instant upload of existing files

### Distributed Coordination
- **Node ID allocation**: Redis SETNX with UUID value, watchdog auto-renewal
- **Scheduled tasks**: Redis SETNX locks with TTL to prevent duplicate execution
- **Heartbeat aggregation**: Async channel + pipeline to prevent Redis request storms

## Database Schema Notes

- `users`: pg_trgm GIN index on username for fuzzy search
- `messages`: Global message log, indexed by `req_id` for idempotency
- `timeline`: Per-user message mailbox, primary key is `(owner_id, seq_id)`
- `timeline_task`: Bridge table for async timeline writing
- `groups`/`group_members`: Group chat support
- `file_record`: File metadata with hash-based deduplication

## Configuration Files

- `instant-messaging/src/main/resources/application.yml`: Main service config
- `websocket-gateway/secret.go`: Contains JWT secret and RabbitMQ password
- Database init: `sql/create_instant_messaging.sql`

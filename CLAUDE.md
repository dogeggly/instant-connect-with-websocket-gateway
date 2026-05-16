# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a distributed instant messaging system with WebSocket gateway architecture. The system uses a microservices approach with three main components:

1. **websocket-gateway** (Go, port 8082): Handles WebSocket connections, message routing, and real-time push notifications
2. **instant-messaging** (Java/Spring Boot, port 8000): REST API for user management, contacts, groups, and message handling
3. **store-consumer** (Java/Spring Boot, port 8001): RabbitMQ consumer that writes messages to user timeline tables asynchronously

## Key Technologies

- **Go 1.26**: websocket-gateway uses Gorilla WebSocket, Redis (go-redis), RabbitMQ (amqp091-go), etcd, Protobuf
- **Java 21**: Business logic uses Spring Boot 3.5.9, MyBatis-Plus (3.5.15), Redisson (3.27.2), JJWT (0.11.5), Hutool (5.8.40)
- **PostgreSQL**: Primary database with pg_trgm extension for fuzzy search and tsvector for full-text search
- **Redis**: Connection routing, online status, distributed locks, seq_id allocation, group member caching
- **RabbitMQ**: Message queue for gateway communication and async timeline processing
- **etcd**: Node registration and service discovery (migrated from Redis)
- **Protocol Buffers**: Message serialization between services (proto3, package `instant_messaging_with_websocket_gateway`)
- **MinIO/S3**: File storage for large file uploads with resumable transfers (AWS SDK v2)

## Common Commands

### Build

```bash
# Go gateway
cd websocket-gateway
go build -o dist/websocket-gateway.exe .    # Windows
go build -o dist/IMWWS .                    # Linux

# Java services
cd instant-messaging  # or store-consumer
mvn package                                  # Build executable JAR
```

### Run (Development)

```bash
# Go gateway
cd websocket-gateway && go run .

# Java services
cd instant-messaging && mvn spring-boot:run
cd store-consumer && mvn spring-boot:run
```

### Protocol Buffer Compilation

Two proto files need compilation — one shared by Go+Java (gateway push), one Java-only (timeline store):

```bash
cd protobuf

# MQ push payload (Go + Java)
protoc --go_out=../websocket-gateway --go_opt=paths=source_relative \
       --java_out=../instant-messaging/src/main/java \
       mq_payload.proto

# Timeline store payload (Java only — both services)
protoc --java_out=../instant-messaging/src/main/java mq_store_payload.proto
protoc --java_out=../store-consumer/src/main/java mq_store_payload.proto
```

### Load Testing

```bash
cd websocket-gateway/test
go run test.go    # 10,000 concurrent WebSocket connections stress test
```

## Architecture

### Message Flow

1. Client sends message via REST API to `instant-messaging` (JWT authenticated)
2. `MessagesServiceImpl.saveMessage()` wraps in `@Transactional`: writes to `messages` table (idempotent by `req_id`) + `timeline_task` table, then async-pushes to MQ
3. `StoreTimelineTask` (scheduled every 1s, Redisson lock `im:task:store`): scans `timeline_task` for unprocessed rows, sends `MqStorePayload` protobuf to `im.direct.store.exchange` (routing key: `store`), deletes confirmed rows
4. `instant-messaging` queries Redis for user routing (`ws:route:{userId}:{deviceId}`) and publishes `MqPayload` to RabbitMQ (direct exchange for single chat, fanout for group)
5. `websocket-gateway` consumes from RabbitMQ and pushes to connected clients
6. `store-consumer` consumes `MqStorePayload` from `im.store.queue`, allocates `seq_id` via Redis Lua script (idempotent: checks `im:seq_id:{ownerId}:last` hash first), batch-inserts to `timeline` table with `ON CONFLICT DO NOTHING`

### RabbitMQ Exchange Topology

| Exchange | Type | Routing Key | Purpose |
|---|---|---|---|
| `im.direct.exchange` | direct | `{nodeId}` | Push messages to specific gateway node |
| `im.fanout.exchange` | fanout | (all) | Broadcast group messages to all gateways |
| `im.direct.store.exchange` | direct | `store` | Timeline write tasks to store-consumer |
| `im.error.exchange` | direct | `error` | Dead letter queue for failed timeline writes |

### Message Routing Strategy

- **Direct messages**: Query Redis ZSET `ws:online:{userId}` for online devices, pipeline batch lookup `ws:route:{userId}:{deviceId}` for gateway node ID, publish to `im.direct.exchange` with routing key = nodeId
- **Group messages**: Query Redis SET `im:group_members:{groupId}` (cache from DB), publish to `im.fanout.exchange`, each gateway matches against its local connections
- **Offline messages**: Users pull from `timeline` table using `seq_id` cursor (not `msg_id`)

### store-consumer: Seq ID Allocation

The `RabbitmqListener` uses a Lua script that:
1. Checks `im:seq_id:{ownerId}:last` hash for existing `msgId -> seqId` mapping (idempotency, TTL 1 hour)
2. If not found, `INCR im:seq_id:{ownerId}` for a new monotonically increasing seq_id
3. Records the mapping and returns the seq_id
4. Batch inserts into `timeline` with `ON CONFLICT (owner_id, seq_id) DO NOTHING`
5. Failed messages go through Spring Retry (3 attempts, exponential backoff) then to dead letter queue

### Key Gateway Components

| File | Component | Role |
|---|---|---|
| `ws_handler.go` | client struct + bufferPool | WebSocket upgrade, per-connection write buffer (sync.Pool), CAS-based flush goroutine |
| `connect_manager.go` | connectionManager | 256-shard map (`[256]*Shard`), FNV-1a hash with AND operation for shard selection, mutex per shard |
| `redis_manager.go` | redisManager | Route/online/bitmap management via atomic Lua scripts, CAS-based register/keepalive/unregister |
| `renew_aggregator.go` | RenewAggregator | Batches heartbeat renewals (channel + Redis Pipeline), flushes every 1s or 500 requests |
| `time_wheel.go` | TimeWheel | 60-slot heartbeat timeout detection, client-side heartbeat every 45s, checks and kicks stale connections |
| `mq_manager.go` | RabbitMQ consumer | Declares exclusive auto-delete queue, deserializes Protobuf, dispatches CHAT_MSG/SYS_KICK_OUT |
| `etcd_manager.go` | EtcdManager | Node registration with 5s lease TTL, lease ID used as nodeId |
| `route_cleanup.go` | Cleanup | Graceful shutdown: iterates all shards, unregisters all local clients from Redis |

### Critical Design Decisions

**Why `seq_id` instead of `msg_id` for cursors?**
Snowflake IDs can arrive out of order due to network timing. Redis `incr` guarantees monotonically increasing sequence IDs per user, preventing message gaps when pulling offline messages.

**Why separate `timeline_task` and `timeline` tables (Outbox pattern)?**
Decouples message ingestion from timeline writing via MQ. Allows:
- Fast message ACK to client (write to `messages` + `timeline_task` only)
- Async processing via `store-consumer` with retry mechanism
- Sharding: `messages` by hash, `timeline` by `owner_id`

**Connection state management:**
- Each gateway maintains in-memory connection map (`userId:deviceId` -> client), 256 shards with independent mutexes
- Redis stores routing info with CAS using connection ID (snowflake): old connections are auto-closed when replaced
- Client-side heartbeat: client sends text frame every 45s, gateway echoes as pong; receiving MQ messages is NOT considered a heartbeat
- Heartbeat renewal batched via aggregator to prevent Redis request storms

**Connection map sharding:**
- 256 shards (power of 2), FNV-1a hash on userId, AND operation (`hash & 255`) instead of modulo
- Plain mutex per shard (not RWMutex) — with 256 shards, read operations are fast enough that reader-writer lock overhead is unjustified

### Distributed Coordination

- **Node ID allocation (Java)**: Redisson distributed lock + watchdog renewal, competing for 0-31 worker IDs
- **Node registration (Go)**: etcd lease with 5s TTL, lease ID serves as nodeId
- **Scheduled tasks**: Redis SETNX locks with TTL to prevent duplicate execution (e.g., `StoreTimelineTask`)
- **Heartbeat aggregation**: Async channel (capacity 5000) + Redis Pipeline, flushes every 1s or 500 requests

### File Upload with Resumable Transfer

- Uses state machine: 0=uninitialized, 1=initializing, 2=uploading, 3=complete
- Redis CAS prevents duplicate initialization
- MinIO multipart upload with presigned URLs per chunk
- File hash-based deduplication (`file_hash` unique index) for instant upload of existing files

## Configuration Pattern

- **Java services**: Main config in `application.yml`, sensitive values (passwords, JWT secret, S3 keys) in `application-secret.yml` (gitignored), loaded via `spring.profiles.include: secret`
- **Go gateway**: All config as hardcoded constants in source files, with passwords/keys centralized in `secret.go` (gitignored)
- **Database init**: `sql/create_instant_messaging.sql` — contains all DDL with comments
- **Protobuf definitions**: `protobuf/` — `mq_payload.proto` (gateway push, Go+Java) and `mq_store_payload.proto` (timeline store, Java only)

## API Endpoints (instant-messaging)

| Prefix | Controller | Key Operations |
|---|---|---|
| `/users` | UsersController | register (MD5+synchronized), login (JWT access+refresh token), fuzzy search (pg_trgm), online count (Redis BITOP) |
| `/messages` | MessagesController | send (single, group, no-store), sync (seq_id cursor), tombstone |
| `/contacts` | ContactsController | CRUD contacts/friends with alias |
| `/file` | FileRecordController | init (dedup+multipart), presigned part URLs, complete merge |
| `/groups` | GroupsController | (not implemented yet) |
| `/group-members` | GroupMembersController | (not implemented yet) |

Auth: JWT token in `token` header, validated by `TokenInterceptor`. Excluded paths: `/users/register`, `/users/login`, `/users/refresh`.

## Database Schema Notes

- `users`: user_id starts at 10001, pg_trgm GIN index on username for fuzzy search
- `messages`: Global message log, `req_id` unique index for idempotency, `extra_data` as JSONB
- `timeline`: Per-user message mailbox, primary key `(owner_id, seq_id)`, `ON CONFLICT DO NOTHING` on insert
- `timeline_task`: Outbox bridge table, status: 1=pending, 2=processing, 3=failed
- `contacts`: Bidirectional (A adds B writes two rows)
- `groups`/`group_members`: Group chat, `group_name` has pg_trgm index
- `file_record`: File metadata, `file_hash` unique index for deduplication

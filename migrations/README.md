# Database Migrations

This directory contains database migrations for the Chat App.

## What Are Migrations?

Migrations are version-controlled database schema changes. They allow you to:
- Track database changes over time
- Apply schema updates automatically
- Rollback changes if needed
- Keep team members in sync

## Migration Files

```
migrations/
├── 000001_initial_schema.up.sql    ← Create tables
├── 000001_initial_schema.down.sql  ← Rollback (drop tables)
└── README.md
```

### Naming Convention

- Format: `{version}_{description}.{up|down}.sql`
- **version**: Sequential number (000001, 000002, etc.)
- **description**: Short description (snake_case)
- **up.sql**: Apply the migration
- **down.sql**: Rollback the migration

## Usage

### Apply Migrations (Automatic with Docker Compose)

Migrations run automatically when you start the services:

```bash
docker-compose up -d
```

The `migrate` service will:
1. Check current database version
2. Apply any pending migrations
3. Update version tracker

### Manual Migration Commands

**Apply all pending migrations:**
```bash
docker-compose run --rm migrate
```

**Check migration status:**
```bash
docker exec newchat-postgres psql -U chatuser -d chatdb -c "SELECT * FROM schema_migrations;"
```

**Rollback last migration:**
```bash
docker-compose run --rm migrate -path /migrations \
  -database "postgres://chatuser:chatpass@postgres:5432/chatdb?sslmode=disable" \
  down 1
```

**Force specific version (dangerous!):**
```bash
docker-compose run --rm migrate -path /migrations \
  -database "postgres://chatuser:chatpass@postgres:5432/chatdb?sslmode=disable" \
  force 1
```

## Creating New Migrations

### Option 1: Manual Creation

Create two files with the next sequential number:

```bash
# Example: Adding indexes
touch migrations/000002_add_indexes.up.sql
touch migrations/000002_add_indexes.down.sql
```

**000002_add_indexes.up.sql:**
```sql
CREATE INDEX idx_messages_room_created ON messages(room_id, created_at DESC);
CREATE INDEX idx_users_email ON users(email);
```

**000002_add_indexes.down.sql:**
```sql
DROP INDEX IF EXISTS idx_messages_room_created;
DROP INDEX IF EXISTS idx_users_email;
```

### Option 2: Using migrate CLI

Install golang-migrate:
```bash
brew install golang-migrate
```

Create migration:
```bash
migrate create -ext sql -dir migrations -seq add_indexes
```

### Running New Migrations

After creating new migration files:

```bash
# Restart migrate service to apply new migrations
docker-compose run --rm migrate

# Or restart all services
docker-compose restart
```

## Migration Best Practices

### ✅ DO:

- **Always create both up and down migrations**
- **Test migrations on a copy of production data**
- **Make migrations backwards compatible when possible**
- **Use transactions (migrations are automatic in golang-migrate)**
- **Add `IF NOT EXISTS` / `IF EXISTS` for idempotency**
  ```sql
  CREATE INDEX IF NOT EXISTS idx_name ON table(column);
  DROP INDEX IF EXISTS idx_name;
  ```

### ❌ DON'T:

- **Don't modify existing migration files** (create new ones instead)
- **Don't delete old migrations** (needed for version tracking)
- **Don't mix schema changes with data changes** (separate migrations)
- **Don't run migrations manually in production** (use automation)

## Examples

### Example 1: Adding a Column

**up.sql:**
```sql
ALTER TABLE users ADD COLUMN phone VARCHAR(20);
```

**down.sql:**
```sql
ALTER TABLE users DROP COLUMN IF EXISTS phone;
```

### Example 2: Creating an Index

**up.sql:**
```sql
CREATE INDEX IF NOT EXISTS idx_messages_user_created
ON messages(user_id, created_at DESC);
```

**down.sql:**
```sql
DROP INDEX IF EXISTS idx_messages_user_created;
```

### Example 3: Adding a Table

**up.sql:**
```sql
CREATE TABLE IF NOT EXISTS notifications (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT REFERENCES users(id) ON DELETE CASCADE,
    message TEXT NOT NULL,
    read BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

**down.sql:**
```sql
DROP TABLE IF EXISTS notifications;
```

## Troubleshooting

### Migration Failed (dirty = true)

If a migration fails midway:

```bash
# Check status
docker exec newchat-postgres psql -U chatuser -d chatdb \
  -c "SELECT * FROM schema_migrations;"

# Output: version | dirty
#         2       | t      ← Migration 2 failed!
```

**Fix:**
1. Manually fix the issue in database
2. Force version to last known good state:
   ```bash
   docker-compose run --rm migrate force 1
   ```
3. Fix the migration file
4. Re-run migrations

### Fresh Start

To start over with migrations (⚠️ **LOSES ALL DATA**):

```bash
# Stop and remove volumes
docker-compose down -v

# Start fresh (migrations will run automatically)
docker-compose up -d
```

### Migration Version Mismatch

If team member has different migrations:

```bash
# Pull latest code
git pull

# Apply new migrations
docker-compose run --rm migrate
```

## Current Migrations

### 000001_initial_schema

Creates the initial database schema:
- `users` table
- `rooms` table
- `room_members` table (many-to-many)
- `messages` table
- `message_reads` table

Applied on: Project initialization

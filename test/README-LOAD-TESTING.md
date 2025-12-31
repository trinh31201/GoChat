# Load Testing Guide

This guide explains how to properly load test your chat application for **concurrent connections**, not just registration.

---

## Why Two Different Tests?

### ❌ Old Test: `websocket-load-test.js`
- Tests user **registration** (write-heavy, unrealistic)
- Creates 10K new users at once
- Hits database write bottleneck
- **Not realistic** for chat app usage

### ✅ New Test: `concurrent-connections-test.js`
- Tests **concurrent WebSocket connections** (realistic)
- Users stay connected for 3-5 minutes
- Sends messages at realistic intervals
- Measures actual chat performance

---

## Test Setup

### Prerequisites

1. **k6 installed**
   ```bash
   brew install k6  # macOS
   ```

2. **Servers running**
   ```bash
   docker-compose up -d
   ```

3. **Room 1 exists**
   ```sql
   -- Connect to database
   psql postgresql://chatuser:chatpass@localhost:5433/chatdb

   -- Check if room exists
   SELECT * FROM rooms WHERE id = 1;

   -- Create if needed
   INSERT INTO rooms (id, name, description, created_at, updated_at)
   VALUES (1, 'Test Room', 'Main load test room', NOW(), NOW());
   ```

---

## Running the Tests

### Step 1: Create Test Users (Run ONCE)

This creates 2000 test users in your database:

```bash
cd test
k6 run create-test-users.js
```

**Expected Output:**
```
Users created: 2000
Success rate:  100%
Duration:      ~2-3 minutes

These users can now be used for load testing:
- Username: testuser0 to testuser1999
- Password: testpass123
- All joined room 1
```

**Notes:**
- Run this ONCE before testing
- Users are created slowly to avoid overwhelming DB
- All users auto-join room 1
- If it fails, check your servers are running

---

### Step 2: Run Concurrent Connection Test

This tests your actual chat capacity:

```bash
k6 run concurrent-connections-test.js 2>&1 | tee test-results-concurrent.txt
```

**Test Stages:**
```
0 → 100 users   (1 min)   - Warm up
100 → 500       (1 min)   - Ramp up
500 → 1000      (2 min)   - Reach 1K
1000            (5 min)   - HOLD 1K concurrent (CRITICAL)
1000 → 1500     (2 min)   - Push further
1500            (3 min)   - Hold 1.5K
1500 → 2000     (2 min)   - Push to 2K
2000            (3 min)   - Hold 2K
2000 → 0        (1 min)   - Ramp down

Total: ~20 minutes
```

**What It Tests:**
- ✅ Concurrent WebSocket connections (up to 2000)
- ✅ Message broadcasting (all users in room 1)
- ✅ Connection stability (users stay connected 3-5 min)
- ✅ Message latency (round-trip time)
- ✅ Server resource usage (CPU, memory)

---

## Interpreting Results

### Success Criteria

**For 1000 Concurrent Users:**
```
✅ Active Connections:     1000+
✅ Message Latency (p95):  < 1000ms
✅ Errors:                 < 1%
✅ Connection Duration:    3-5 minutes average
```

**For 2000 Concurrent Users:**
```
⚠️  This is 2x your design capacity (1000-2000)
    Success here means you can handle peaks
```

### Sample Good Results

```
========================================
Concurrent Connection Test Results
========================================

Peak Concurrent Connections: 2000
Total Messages Sent:         15,432
Total Messages Received:     1,543,200  (15k × 100 users)

Message Latency:
  Average:  125 ms
  p50:      98 ms
  p95:      450 ms      ← UNDER 1000ms ✅
  p99:      890 ms
  Max:      1200 ms

Connection Duration:
  Average:  240 seconds  (4 min)
  Max:      300 seconds  (5 min)

Total Errors: 5             ← LOW ✅

Test Duration: 1200 seconds (20 min)
========================================
```

### Sample Bad Results

```
Peak Concurrent Connections: 1200  ← FAILED to reach 2000 ❌
Message Latency p95:         4500 ms  ← TOO SLOW ❌
Total Errors: 1500           ← TOO MANY ❌
```

---

## Monitoring During Test

### Watch Server Logs

**Terminal 1 - Server 1:**
```bash
docker logs -f newchat-app
```

**Terminal 2 - Server 2:**
```bash
# If running second server
docker logs -f newchat-app-2
```

### Watch System Resources

```bash
# CPU and Memory
docker stats

# PostgreSQL connections
docker exec -it newchat-postgres psql -U chatuser -d chatdb -c \
  "SELECT count(*) FROM pg_stat_activity WHERE datname='chatdb';"

# Redis memory
docker exec -it newchat-redis redis-cli INFO memory
```

### Key Metrics to Watch

1. **Active Connections**: Should reach 1000-2000
2. **CPU Usage**: Should stay < 80%
3. **Memory**: Should not grow unbounded
4. **Database Connections**: Should stay < 200 (with PgBouncer) or < 600 (without)
5. **Message Latency**: p95 should be < 1 second

---

## Troubleshooting

### Test Fails: "Login failed"

**Problem:** Test users don't exist

**Solution:**
```bash
# Run user creation first
k6 run create-test-users.js
```

---

### Test Fails: "Room not found"

**Problem:** Room 1 doesn't exist

**Solution:**
```bash
docker exec -it newchat-postgres psql -U chatuser -d chatdb -c \
  "INSERT INTO rooms (id, name, description, created_at, updated_at) \
   VALUES (1, 'Test Room', 'Load test room', NOW(), NOW());"
```

---

### Connections Don't Reach Target

**Problem:** Server capacity limit

**Possible Causes:**
1. **File descriptor limit** (ulimit)
   ```bash
   ulimit -n 10000  # Increase open file limit
   ```

2. **Server connection limit**
   - Check nginx worker_connections (currently 10000)
   - Check Go max connections

3. **Database bottleneck**
   - Add PgBouncer
   - Reduce connection pool size

---

### High Message Latency

**Problem:** p95 > 1000ms

**Possible Causes:**
1. **Redis Pub/Sub slow** - Check Redis CPU
2. **Database slow** - Add indexes, caching
3. **Network** - Check if running on localhost
4. **Server CPU** - Scale horizontally

---

## Comparing Old vs New Test

### Old Test (Registration Storm):
```bash
k6 run websocket-load-test.js
```
- Tests: 10K registrations
- Result: 99% failure (database write bottleneck)
- Conclusion: Unrealistic, found wrong bottleneck

### New Test (Concurrent Connections):
```bash
k6 run concurrent-connections-test.js
```
- Tests: 2K concurrent connections
- Result: Will show REAL capacity
- Conclusion: Realistic, finds actual limits

---

## Next Steps After Testing

Based on results, optimize in this order:

1. **If connections fail < 1000:**
   - Add file descriptor limits
   - Check server configuration
   - Add more servers

2. **If latency > 1000ms:**
   - Add Redis caching (user lookups, room data)
   - Optimize database queries
   - Add database indexes

3. **If errors > 5%:**
   - Add rate limiting
   - Add circuit breakers
   - Improve error handling

4. **If database connections > 600:**
   - Add PgBouncer
   - Reduce connection pool per server
   - Use connection pooling

---

## Summary

**Old Way (Wrong):**
```bash
# ❌ Tests unrealistic registration storm
k6 run websocket-load-test.js
```

**New Way (Correct):**
```bash
# Step 1: Create users (ONCE)
k6 run create-test-users.js

# Step 2: Test concurrent connections
k6 run concurrent-connections-test.js
```

This new approach tests your **actual chat usage patterns** and will reveal your **real capacity limits**.

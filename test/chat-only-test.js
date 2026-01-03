import ws from 'k6/ws';
import { check } from 'k6';
import http from 'k6/http';
import { Counter, Gauge, Trend } from 'k6/metrics';

// Metrics
const activeConnections = new Gauge('active_connections');
const messagesSent = new Counter('messages_sent');
const messagesReceived = new Counter('messages_received');
const errors = new Counter('errors');
const authSuccess = new Counter('auth_success');
const joinSuccess = new Counter('join_success');
const wsLatency = new Trend('ws_latency_ms');

export const options = {
  setupTimeout: '180s',
  scenarios: {
    chat_test: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '15s', target: 1000 },
        { duration: '15s', target: 2500 },
        { duration: '15s', target: 5000 },
        { duration: '15s', target: 7500 },
        { duration: '15s', target: 10000 },
        { duration: '1m', target: 10000 },
        { duration: '15s', target: 0 },
      ],
    },
  },
};

// Use existing users (created from previous tests)
const BASE_URL = 'http://localhost/api/v1';
const WS_URL = 'ws://localhost/ws';
const headers = { 'Content-Type': 'application/json' };

// Pre-login tokens cache (setup phase)
let tokens = [];
let roomId = 1;

export function setup() {
  // Create or get a room for testing
  const adminEmail = 'chattest_admin@test.com';
  const adminPass = 'test123';

  // Try to register admin (ignore if exists)
  http.post(`${BASE_URL}/users/register`, JSON.stringify({
    email: adminEmail,
    username: 'chattest_admin',
    password: adminPass,
  }), { headers });

  // Login admin
  const loginRes = http.post(`${BASE_URL}/users/login`, JSON.stringify({
    email: adminEmail,
    password: adminPass,
  }), { headers });

  if (loginRes.status !== 200) {
    console.log('Admin login failed');
    return { roomId: 1, tokens: [] };
  }

  const adminToken = JSON.parse(loginRes.body).token;

  // Create test room
  const roomRes = http.post(`${BASE_URL}/rooms`, JSON.stringify({
    name: 'Chat Performance Test Room',
    type: 'public',
  }), {
    headers: { ...headers, 'Authorization': `Bearer ${adminToken}` },
  });

  if (roomRes.status === 200) {
    roomId = JSON.parse(roomRes.body).id;
    console.log(`Created room: ${roomId}`);
  }

  // Pre-login 200 users and cache tokens (reused for 10K VUs)
  console.log('Pre-logging in users...');
  const batchTokens = [];

  for (let i = 1; i <= 200; i++) {
    const email = `loadtest${i}@test.com`;
    const loginRes = http.post(`${BASE_URL}/users/login`, JSON.stringify({
      email: email,
      password: 'test123',
    }), { headers });

    if (loginRes.status === 200) {
      const data = JSON.parse(loginRes.body);
      batchTokens.push({ token: data.token, userId: i });

      // Join the room via REST API (required before WebSocket join)
      http.post(`${BASE_URL}/rooms/${roomId}/join`, '{}', {
        headers: { ...headers, 'Authorization': `Bearer ${data.token}` },
      });
    }

    if (i % 50 === 0) {
      console.log(`Logged in and joined ${i} users, got ${batchTokens.length} tokens`);
    }
  }

  console.log(`Setup complete: ${batchTokens.length} users logged in and joined room ${roomId}`);
  return { roomId: parseInt(roomId), tokens: batchTokens };
}

export default function (data) {
  const { roomId, tokens } = data;

  if (tokens.length === 0) {
    errors.add(1);
    return;
  }

  // Pick a random pre-authenticated user
  const userIndex = (__VU - 1) % tokens.length;
  const { token, userId } = tokens[userIndex];

  // Connect WebSocket with pre-authenticated token
  const wsStart = Date.now();

  const res = ws.connect(WS_URL, { timeout: '60s' }, function (socket) {
    activeConnections.add(1);

    socket.on('open', () => {
      wsLatency.add(Date.now() - wsStart);
      socket.send(JSON.stringify({ type: 'auth', token: token }));
    });

    socket.on('message', (msgData) => {
      try {
        const msg = JSON.parse(msgData);

        if (msg.type === 'success' && msg.message && msg.message.includes('Authenticated')) {
          authSuccess.add(1);
          socket.send(JSON.stringify({ type: 'join_room', room_id: roomId }));
        }

        if (msg.type === 'room_joined') {
          joinSuccess.add(1);
          // Send multiple messages
          for (let i = 0; i < 3; i++) {
            socket.send(JSON.stringify({
              type: 'send_message',
              content: `Message ${i} from user ${userId}`,
            }));
            messagesSent.add(1);
          }
        }

        if (msg.type === 'new_message') {
          messagesReceived.add(1);
        }

        if (msg.type === 'error') {
          errors.add(1);
        }
      } catch (e) {
        // JSON parse error
      }
    });

    socket.on('error', (e) => {
      errors.add(1);
    });

    socket.on('close', () => {
      activeConnections.add(-1);
    });

    // Keep connection open 20-30 seconds
    socket.setTimeout(function () {
      socket.close();
    }, 20000 + Math.random() * 10000);
  });

  check(res, { 'ws connected': (r) => r && r.status === 101 });
}

export function handleSummary(data) {
  const summary = `
=====================================
CHAT SERVICE TEST RESULTS (Target: 10K)
=====================================

Peak Connections:  ${data.metrics.active_connections?.values?.max || 0}
Auth Success:      ${data.metrics.auth_success?.values?.count || 0}
Join Success:      ${data.metrics.join_success?.values?.count || 0}
Messages Sent:     ${data.metrics.messages_sent?.values?.count || 0}
Messages Received: ${data.metrics.messages_received?.values?.count || 0}
Errors:            ${data.metrics.errors?.values?.count || 0}

WebSocket Latency:
  avg: ${(data.metrics.ws_latency_ms?.values?.avg || 0).toFixed(0)}ms
  p95: ${(data.metrics.ws_latency_ms?.values['p(95)'] || 0).toFixed(0)}ms

Test Duration: ${(data.state.testRunDurationMs / 1000).toFixed(0)} seconds
=====================================
`;
  console.log(summary);
  return { 'stdout': summary };
}

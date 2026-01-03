import ws from 'k6/ws';
import { check } from 'k6';
import http from 'k6/http';
import { Counter, Gauge, Trend } from 'k6/metrics';

const activeConnections = new Gauge('active_connections');
const messagesSent = new Counter('messages_sent');
const messagesReceived = new Counter('messages_received');
const errors = new Counter('errors');
const authSuccess = new Counter('auth_success');
const joinSuccess = new Counter('join_success');
const connectLatency = new Trend('connect_latency_ms');

export const options = {
  scenarios: {
    scale_test: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '15s', target: 100 },
        { duration: '15s', target: 250 },
        { duration: '15s', target: 500 },
        { duration: '1m', target: 500 },
        { duration: '15s', target: 0 },
      ],
    },
  },
};

const BASE_URL = 'http://localhost/api/v1';
const WS_URL = 'ws://localhost/ws';
const headers = { 'Content-Type': 'application/json' };

export function setup() {
  http.post(`${BASE_URL}/users/register`, JSON.stringify({
    email: 'loadtest_admin@test.com',
    username: 'loadtest_admin',
    password: 'test123',
  }), { headers });

  const loginRes = http.post(`${BASE_URL}/users/login`, JSON.stringify({
    email: 'loadtest_admin@test.com',
    password: 'test123',
  }), { headers });

  const token = JSON.parse(loginRes.body).token;

  const roomRes = http.post(`${BASE_URL}/rooms`, JSON.stringify({
    name: 'Load Test Room 500',
    type: 'public',
  }), {
    headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${token}` },
  });

  let roomId = 1;
  if (roomRes.status === 200) {
    roomId = JSON.parse(roomRes.body).id;
    console.log(`Created room: ${roomId}`);
  }

  return { roomId: parseInt(roomId) };
}

export default function (data) {
  const roomId = data.roomId;
  const userId = __VU;
  const email = `loadtest${userId}@test.com`;
  const password = 'test123';
  const username = `loadtest${userId}`;

  // Register
  http.post(`${BASE_URL}/users/register`, JSON.stringify({
    email, username, password,
  }), { headers });

  // Login
  const loginStart = Date.now();
  const loginRes = http.post(`${BASE_URL}/users/login`, JSON.stringify({
    email, password,
  }), { headers });

  if (loginRes.status !== 200) {
    errors.add(1);
    return;
  }

  const token = JSON.parse(loginRes.body).token;
  connectLatency.add(Date.now() - loginStart);

  const authHeaders = { 'Content-Type': 'application/json', 'Authorization': `Bearer ${token}` };

  // Join room via REST
  http.post(`${BASE_URL}/rooms/${roomId}/join`, '{}', { headers: authHeaders });

  // Connect WebSocket (NO sleep() - use setTimeout instead!)
  const res = ws.connect(WS_URL, { timeout: '90s' }, function (socket) {
    activeConnections.add(1);

    socket.on('open', () => {
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
          socket.send(JSON.stringify({
            type: 'send_message',
            content: `Hello from user ${userId}`,
          }));
          messagesSent.add(1);
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

    // Keep connection open 30-45 seconds using setTimeout (not sleep!)
    socket.setTimeout(function () {
      socket.close();
    }, 30000 + Math.random() * 15000);
  });

  check(res, { 'ws connected': (r) => r && r.status === 101 });
}

export function handleSummary(data) {
  const summary = `
=====================================
SCALE TEST RESULTS (Target: 500)
=====================================

Peak Connections:  ${data.metrics.active_connections?.values?.max || 0}
Auth Success:      ${data.metrics.auth_success?.values?.count || 0}
Join Success:      ${data.metrics.join_success?.values?.count || 0}
Messages Sent:     ${data.metrics.messages_sent?.values?.count || 0}
Messages Received: ${data.metrics.messages_received?.values?.count || 0}
Errors:            ${data.metrics.errors?.values?.count || 0}

Connect Latency:
  avg: ${(data.metrics.connect_latency_ms?.values?.avg || 0).toFixed(0)}ms
  p95: ${(data.metrics.connect_latency_ms?.values['p(95)'] || 0).toFixed(0)}ms

HTTP Request Duration:
  avg: ${(data.metrics.http_req_duration?.values?.avg || 0).toFixed(0)}ms
  p95: ${(data.metrics.http_req_duration?.values['p(95)'] || 0).toFixed(0)}ms

Test Duration: ${(data.state.testRunDurationMs / 1000).toFixed(0)} seconds
=====================================
`;
  console.log(summary);
  return { 'stdout': summary };
}

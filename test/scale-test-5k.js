import ws from 'k6/ws';
import { check, sleep } from 'k6';
import http from 'k6/http';
import { Counter, Gauge, Trend } from 'k6/metrics';

// Custom metrics
const activeConnections = new Gauge('active_connections');
const messagesSent = new Counter('messages_sent');
const errors = new Counter('errors');
const connectLatency = new Trend('connect_latency_ms');

// Test: Ramp up to 5,000 concurrent connections
export const options = {
  scenarios: {
    scale_test: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '30s', target: 500 },    // Warm up
        { duration: '30s', target: 1000 },   // 1K
        { duration: '30s', target: 2000 },   // 2K
        { duration: '30s', target: 3000 },   // 3K
        { duration: '30s', target: 4000 },   // 4K
        { duration: '30s', target: 5000 },   // 5K
        { duration: '2m', target: 5000 },    // Hold 5K for 2 min
        { duration: '30s', target: 0 },      // Ramp down
      ],
    },
  },
};

// Go through nginx load balancer
const BASE_URL = 'http://localhost/api/v1';
const WS_URL = 'ws://localhost/ws';

export default function () {
  const userId = __VU;
  const email = `loadtest${userId}@test.com`;
  const password = 'test123';
  const username = `loadtest${userId}`;

  // Register (ignore if exists)
  http.post(`${BASE_URL}/users/register`, JSON.stringify({
    email: email,
    username: username,
    password: password,
  }), { headers: { 'Content-Type': 'application/json' } });

  // Login
  const loginStart = Date.now();
  const loginRes = http.post(`${BASE_URL}/users/login`, JSON.stringify({
    email: email,
    password: password,
  }), { headers: { 'Content-Type': 'application/json' } });

  if (loginRes.status !== 200) {
    errors.add(1);
    sleep(1);
    return;
  }

  const token = JSON.parse(loginRes.body).token;
  connectLatency.add(Date.now() - loginStart);

  // Connect WebSocket
  const res = ws.connect(WS_URL, {}, function (socket) {
    socket.on('open', () => {
      activeConnections.add(1);

      // Auth
      socket.send(JSON.stringify({ type: 'auth', token: token }));
    });

    socket.on('message', (data) => {
      const msg = JSON.parse(data);

      if (msg.type === 'success' && msg.message.includes('Authenticated')) {
        // Join room
        socket.send(JSON.stringify({ type: 'join_room', room_id: 1 }));
      }

      if (msg.type === 'room_joined') {
        // Send a message
        socket.send(JSON.stringify({
          type: 'send_message',
          content: `Hello from user ${userId}`,
        }));
        messagesSent.add(1);
      }
    });

    socket.on('error', () => {
      errors.add(1);
      activeConnections.add(-1);
    });

    socket.on('close', () => {
      activeConnections.add(-1);
    });

    // Stay connected for 60-90 seconds
    sleep(60 + Math.random() * 30);
    socket.close();
  });

  check(res, { 'ws connected': (r) => r && r.status === 101 });
}

export function handleSummary(data) {
  const summary = `
=====================================
SCALE TEST RESULTS (Target: 5,000)
=====================================

Peak Connections:  ${data.metrics.active_connections?.values?.max || 0}
Messages Sent:     ${data.metrics.messages_sent?.values?.count || 0}
Errors:            ${data.metrics.errors?.values?.count || 0}

Connect Latency:
  avg: ${(data.metrics.connect_latency_ms?.values?.avg || 0).toFixed(0)}ms
  p95: ${(data.metrics.connect_latency_ms?.values['p(95)'] || 0).toFixed(0)}ms

HTTP Request Duration:
  avg: ${(data.metrics.http_req_duration?.values?.avg || 0).toFixed(0)}ms
  p95: ${(data.metrics.http_req_duration?.values['p(95)'] || 0).toFixed(0)}ms

Test Duration: ${(data.state.testRunDurationMs / 1000 / 60).toFixed(1)} minutes
=====================================
`;
  console.log(summary);
  return { 'stdout': summary };
}

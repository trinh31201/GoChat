import ws from 'k6/ws';
import { check, sleep } from 'k6';
import http from 'k6/http';
import { Counter, Gauge, Trend } from 'k6/metrics';

// ========================================
// REALISTIC CONCURRENT CONNECTION TEST
// ========================================
// This test simulates real chat usage:
// - Users login (not register)
// - Stay connected for extended periods
// - Send messages at realistic intervals
// - Measure concurrent connection capacity
// ========================================

// Custom metrics
const activeConnections = new Gauge('active_websocket_connections');
const messagesSent = new Counter('messages_sent_total');
const messagesReceived = new Counter('messages_received_total');
const messageLatency = new Trend('message_round_trip_latency_ms');
const connectionDuration = new Trend('connection_duration_seconds');
const errors = new Counter('errors_total');

// Test configuration for concurrent connections
export const options = {
  scenarios: {
    concurrent_connections: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '1m', target: 100 },      // Warm up: 100 users
        { duration: '1m', target: 500 },      // Ramp to 500
        { duration: '2m', target: 1000 },     // Ramp to 1K
        { duration: '5m', target: 1000 },     // Hold 1K for 5 min (SUSTAINED)
        { duration: '2m', target: 1500 },     // Push to 1.5K
        { duration: '3m', target: 1500 },     // Hold 1.5K
        { duration: '2m', target: 2000 },     // Push to 2K
        { duration: '3m', target: 2000 },     // Hold 2K
        { duration: '1m', target: 0 },        // Ramp down
      ],
      gracefulRampDown: '30s',
    },
  },
  thresholds: {
    'active_websocket_connections': ['value>0'],
    'message_round_trip_latency_ms': ['p(95)<1000'], // 95% under 1 second
    'errors_total': ['count<100'], // Less than 100 errors total
  },
};

// Test directly against backend (bypassing nginx for now)
const BASE_URL = 'http://localhost:8001/api/v1';
const WS_URL = 'ws://localhost:8001/ws';

export default function () {
  // Use pre-created k6 users from previous test
  const userId = (__VU + __ITER * 10000) % 5000; // Cycle through 5000 k6users
  const username = `k6user_${Math.floor(userId / 10)}_${userId % 10}`;
  const password = 'testpass123';

  // Step 1: Login (NOT register!)
  const loginRes = http.post(
    `${BASE_URL}/users/login`,
    JSON.stringify({
      email: `${username}@loadtest.com`,
      password: password,
    }),
    { headers: { 'Content-Type': 'application/json' } }
  );

  const loginOk = check(loginRes, {
    'login successful': (r) => r.status === 200,
  });

  if (!loginOk) {
    errors.add(1);
    console.error(`Login failed for ${username}: ${loginRes.status}`);
    sleep(1);
    return;
  }

  const userData = JSON.parse(loginRes.body);
  const token = userData.token;

  // Step 2: Connect WebSocket and STAY CONNECTED
  const connectionStart = Date.now();
  let messagesReceivedCount = 0;
  let connectionActive = true;

  const res = ws.connect(WS_URL, { tags: { name: 'ConcurrentTest' } }, function (socket) {
    socket.on('open', () => {
      activeConnections.add(1);

      // Authenticate
      socket.send(JSON.stringify({
        type: 'auth',
        token: token,
      }));
    });

    socket.on('message', (data) => {
      const msg = JSON.parse(data);
      messagesReceivedCount++;
      messagesReceived.add(1);

      // Join room after successful auth
      if (msg.type === 'success') {
        socket.send(JSON.stringify({
          type: 'join_room',
          room_id: 1,
        }));
      }

      // After joining room, start realistic chat behavior
      if (msg.type === 'room_joined') {
        // Simulate realistic user behavior
        simulateRealisticChatBehavior(socket, username);
      }

      // Track message latency if this is a response to our message
      if (msg.type === 'new_message' && msg.data && msg.data.content && msg.data.content.includes(`from ${username}`)) {
        const now = Date.now();
        const sent = parseInt(msg.data.content.split('timestamp:')[1]);
        if (sent) {
          messageLatency.add(now - sent);
        }
      }
    });

    socket.on('error', (e) => {
      errors.add(1);
      activeConnections.add(-1);
      console.error(`WebSocket error for ${username}: ${e.error()}`);
    });

    socket.on('close', () => {
      if (connectionActive) {
        activeConnections.add(-1);
        const duration = (Date.now() - connectionStart) / 1000;
        connectionDuration.add(duration);
        connectionActive = false;
      }
    });

    // Keep connection alive for realistic duration (3-5 minutes)
    const connectionDurationSec = 180 + Math.random() * 120; // 3-5 minutes
    socket.setTimeout(() => {
      if (connectionActive) {
        socket.close();
      }
    }, connectionDurationSec * 1000);

    // Heartbeat to keep connection alive
    const heartbeatInterval = socket.setInterval(() => {
      if (connectionActive) {
        socket.send(JSON.stringify({ type: 'ping' }));
      }
    }, 30000); // Every 30 seconds
  });

  check(res, {
    'websocket connected': (r) => r && r.status === 101,
  });

  // Don't immediately start another connection
  // Let this user stay connected for a while
  sleep(180 + Math.random() * 120); // Match connection duration
}

// Simulate realistic chat behavior
function simulateRealisticChatBehavior(socket, username) {
  let messageCount = 0;
  const maxMessages = 5 + Math.floor(Math.random() * 10); // 5-15 messages per session

  // Send messages at realistic intervals
  const sendMessage = () => {
    if (messageCount >= maxMessages) {
      return;
    }

    // Random interval between messages (10-60 seconds)
    const nextMessageDelay = 10000 + Math.random() * 50000;

    socket.setTimeout(() => {
      const timestamp = Date.now();
      const content = `Message ${messageCount} from ${username} timestamp:${timestamp}`;

      socket.send(JSON.stringify({
        type: 'send_message',
        content: content,
      }));

      messagesSent.add(1);
      messageCount++;

      // Schedule next message
      sendMessage();
    }, nextMessageDelay);
  };

  // Start sending messages
  sendMessage();
}

export function handleSummary(data) {
  const summary = `
========================================
Concurrent Connection Test Results
========================================

Peak Concurrent Connections: ${data.metrics.active_websocket_connections?.values?.max || 0}
Total Messages Sent:         ${data.metrics.messages_sent_total?.values?.count || 0}
Total Messages Received:     ${data.metrics.messages_received_total?.values?.count || 0}

Message Latency:
  Average:  ${(data.metrics.message_round_trip_latency_ms?.values?.avg || 0).toFixed(2)} ms
  p50:      ${(data.metrics.message_round_trip_latency_ms?.values?.med || 0).toFixed(2)} ms
  p95:      ${(data.metrics.message_round_trip_latency_ms?.values['p(95)'] || 0).toFixed(2)} ms
  p99:      ${(data.metrics.message_round_trip_latency_ms?.values['p(99)'] || 0).toFixed(2)} ms
  Max:      ${(data.metrics.message_round_trip_latency_ms?.values?.max || 0).toFixed(2)} ms

Connection Duration:
  Average:  ${(data.metrics.connection_duration_seconds?.values?.avg || 0).toFixed(2)} seconds
  Max:      ${(data.metrics.connection_duration_seconds?.values?.max || 0).toFixed(2)} seconds

Total Errors: ${data.metrics.errors_total?.values?.count || 0}

Test Duration: ${(data.state.testRunDurationMs / 1000).toFixed(2)} seconds
========================================
`;

  console.log(summary);

  return {
    'stdout': summary,
    'test-results-concurrent.txt': summary,
  };
}

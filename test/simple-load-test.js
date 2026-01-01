import http from 'k6/http';
import ws from 'k6/ws';
import { check, sleep } from 'k6';
import { Counter, Gauge } from 'k6/metrics';

// Custom metrics
const activeConnections = new Gauge('active_connections');
const messagesSent = new Counter('messages_sent');
const errors = new Counter('errors');

// Configure: which server to test
const SERVER_PORT = __ENV.PORT || '80';
const BASE_URL = `http://localhost:${SERVER_PORT}/api/v1`;
const WS_URL = `ws://localhost:${SERVER_PORT}/ws`;

export const options = {
  stages: [
    { duration: '30s', target: 50 },    // Ramp to 50
    { duration: '1m', target: 50 },     // Hold 50
    { duration: '30s', target: 0 },     // Ramp down
  ],
};

export default function () {
  // Unique user per VU + iteration
  const uniqueId = `${__VU}_${__ITER}_${Date.now()}`;
  const username = `user_${uniqueId}`;

  // Step 1: Register user
  const registerRes = http.post(
    `${BASE_URL}/users/register`,
    JSON.stringify({
      username: username,
      email: `${username}@test.com`,
      password: 'test123',
    }),
    { headers: { 'Content-Type': 'application/json' } }
  );

  if (registerRes.status !== 200) {
    errors.add(1);
    sleep(1);
    return;
  }

  const userData = JSON.parse(registerRes.body);
  const token = userData.token;
  const userId = userData.user.id;

  // Step 2: Create a room for this user
  const createRoomRes = http.post(
    `${BASE_URL}/rooms`,
    JSON.stringify({
      name: `room_${uniqueId}`,
      type: 'public',
    }),
    {
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${token}`,
      }
    }
  );

  let roomId = 1; // Default fallback
  if (createRoomRes.status === 200) {
    const roomData = JSON.parse(createRoomRes.body);
    roomId = parseInt(roomData.id); // API returns id at top level
  }

  // Step 3: Connect WebSocket
  const res = ws.connect(WS_URL, {}, function (socket) {
    let authenticated = false;
    let roomJoined = false;

    socket.on('open', () => {
      activeConnections.add(1);
      // Authenticate first
      socket.send(JSON.stringify({ type: 'auth', token: token }));
    });

    socket.on('message', (msg) => {
      const data = JSON.parse(msg);

      // After auth success, join room
      if (data.type === 'success' && !authenticated) {
        authenticated = true;
        socket.send(JSON.stringify({
          type: 'join_room',
          room_id: roomId,
        }));
      }

      // After joining room, send a message immediately
      if (data.type === 'room_joined' && !roomJoined) {
        roomJoined = true;
        messagesSent.add(1);
        socket.send(JSON.stringify({
          type: 'send_message',
          content: `Hello from ${username}`,
        }));
      }
    });

    socket.on('close', () => {
      activeConnections.add(-1);
    });

    socket.on('error', (e) => {
      errors.add(1);
      activeConnections.add(-1);
    });

    // Stay connected for 30-60 seconds
    sleep(30 + Math.random() * 30);
  });
}

export function handleSummary(data) {
  return {
    'stdout': `
========================================
Load Test Results - Server :${SERVER_PORT}
========================================

Peak Connections: ${data.metrics.active_connections ? data.metrics.active_connections.values.max : 0}
Messages Sent:    ${data.metrics.messages_sent ? data.metrics.messages_sent.values.count : 0}
Errors:           ${data.metrics.errors ? data.metrics.errors.values.count : 0}
Duration:         ${Math.round(data.state.testRunDurationMs / 1000)}s

HTTP Requests:
  Total:    ${data.metrics.http_reqs ? data.metrics.http_reqs.values.count : 0}
  Failed:   ${data.metrics.http_req_failed ? data.metrics.http_req_failed.values.passes : 0}

WebSocket:
  Sessions: ${data.metrics.ws_sessions ? data.metrics.ws_sessions.values.count : 0}

========================================
`,
  };
}

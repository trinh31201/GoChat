import http from 'k6/http';
import ws from 'k6/ws';
import { check, sleep } from 'k6';
import { Counter, Gauge } from 'k6/metrics';

const activeConnections = new Gauge('active_connections');
const messagesSent = new Counter('messages_sent');
const errors = new Counter('errors');

const SERVER_PORT = __ENV.PORT || '8001';
const BASE_URL = `http://localhost:${SERVER_PORT}/api/v1`;
const WS_URL = `ws://localhost:${SERVER_PORT}/ws`;

export const options = {
  stages: [
    { duration: '10s', target: 50 },
    { duration: '20s', target: 100 },
    { duration: '10s', target: 100 },
    { duration: '10s', target: 0 },
  ],
};

export default function () {
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
    return;
  }

  const userData = JSON.parse(registerRes.body);
  const token = userData.token;
  const userId = userData.user.id;

  // Step 2: Create a room
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

  let roomId = 1;
  if (createRoomRes.status === 200) {
    const roomData = JSON.parse(createRoomRes.body);
    roomId = parseInt(roomData.id);
  } else {
    errors.add(1);
    return;
  }

  // Step 3: Creator is automatically a member when they create the room
  // No need to call join API

  // Step 4: Connect WebSocket
  const res = ws.connect(WS_URL, {}, function (socket) {
    let authenticated = false;
    let roomJoined = false;

    socket.on('open', () => {
      activeConnections.add(1);
      socket.send(JSON.stringify({ type: 'auth', token: token }));
    });

    socket.on('message', (msg) => {
      const data = JSON.parse(msg);

      if (data.type === 'success' && !authenticated) {
        authenticated = true;
        socket.send(JSON.stringify({
          type: 'join_room',
          room_id: roomId,
        }));
      }

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

    socket.on('error', () => {
      errors.add(1);
      activeConnections.add(-1);
    });

    // Stay connected for 1-2 seconds
    sleep(1 + Math.random() * 1);
  });

  check(res, { 'ws connected': (r) => r && r.status === 101 });
}

export function handleSummary(data) {
  return {
    'stdout': `
========================================
Basic Load Test Results (Old Version)
========================================

Peak Connections: ${data.metrics.active_connections ? data.metrics.active_connections.values.max : 0}
Messages Sent:    ${data.metrics.messages_sent ? data.metrics.messages_sent.values.count : 0}
Errors:           ${data.metrics.errors ? data.metrics.errors.values.count : 0}
Duration:         ${Math.round(data.state.testRunDurationMs / 1000)}s

HTTP Requests:
  Total:    ${data.metrics.http_reqs ? data.metrics.http_reqs.values.count : 0}
  Duration: ${data.metrics.http_req_duration ? Math.round(data.metrics.http_req_duration.values.avg) : 0}ms avg

WebSocket:
  Sessions: ${data.metrics.ws_sessions ? data.metrics.ws_sessions.values.count : 0}

========================================
`,
  };
}

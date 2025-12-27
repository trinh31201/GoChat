import ws from 'k6/ws';
import { check, sleep } from 'k6';
import http from 'k6/http';
import { Counter, Trend } from 'k6/metrics';

// Custom metrics
const wsConnections = new Counter('websocket_connections_total');
const wsMessagesSent = new Counter('websocket_messages_sent');
const wsMessagesReceived = new Counter('websocket_messages_received');
const wsErrors = new Counter('websocket_errors');

// Test configuration
export const options = {
  stages: [
    { duration: '10s', target: 20 },   // Warm up to 20 users
    { duration: '20s', target: 50 },   // Ramp to 50 users
    { duration: '20s', target: 100 },  // Ramp to 100 users
    { duration: '10s', target: 100 },  // Stay at 100
    { duration: '10s', target: 0 },    // Ramp down
  ],
};

const BASE_URL = 'http://localhost:8001/api/v1';
const WS_URL = 'ws://localhost:8001/ws';

export default function () {
  // Generate unique user
  const username = `k6user_${__VU}_${__ITER}`;
  const email = `${username}@loadtest.com`;
  const password = 'testpass123';

  // Step 1: Register user
  const registerRes = http.post(
    `${BASE_URL}/users/register`,
    JSON.stringify({
      username: username,
      email: email,
      password: password,
    }),
    { headers: { 'Content-Type': 'application/json' } }
  );

  const registerOk = check(registerRes, {
    'user registered': (r) => r.status === 200,
  });

  if (!registerOk) {
    wsErrors.add(1);
    return;
  }

  const userData = JSON.parse(registerRes.body);
  const token = userData.token;
  const userId = userData.user.id;

  // Step 2: Join room via API (required before WebSocket join)
  const joinRoomRes = http.post(
    `${BASE_URL}/rooms/1/join`,
    JSON.stringify({
      user_id: userId,
      room_id: 1
    }),
    {
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${token}`
      }
    }
  );

  const joinOk = check(joinRoomRes, {
    'joined room': (r) => r.status === 200
  });

  if (!joinOk) {
    wsErrors.add(1);
    return;
  }

  // Step 3: Connect WebSocket
  const params = { tags: { name: 'WebSocketTest' } };

  const res = ws.connect(WS_URL, params, function (socket) {
    socket.on('open', () => {
      wsConnections.add(1);

      // Authenticate
      socket.send(JSON.stringify({
        type: 'auth',
        token: token,
      }));
    });

    socket.on('message', (data) => {
      const msg = JSON.parse(data);
      wsMessagesReceived.add(1);

      // Join room after successful auth
      if (msg.type === 'success') {
        socket.send(JSON.stringify({
          type: 'join_room',
          room_id: 1, // Assumes room 1 exists
        }));
      }

      // Send messages after joining room
      if (msg.type === 'room_joined') {
        // Send 5 messages
        for (let i = 0; i < 5; i++) {
          socket.send(JSON.stringify({
            type: 'send_message',
            content: `Test message ${i} from ${username}`,
          }));
          wsMessagesSent.add(1);
        }

        // Close after sending messages
        socket.setTimeout(() => {
          socket.close();
        }, 2000);
      }
    });

    socket.on('error', (e) => {
      wsErrors.add(1);
      console.error(`WebSocket error: ${e.error()}`);
    });

    // Timeout after 15 seconds
    socket.setTimeout(() => {
      socket.close();
    }, 15000);
  });

  check(res, {
    'websocket connected': (r) => r && r.status === 101,
  });

  sleep(1);
}

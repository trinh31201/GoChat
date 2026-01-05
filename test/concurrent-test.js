import http from 'k6/http';
import ws from 'k6/ws';
import { check, sleep } from 'k6';
import { Counter, Gauge, Trend, Rate } from 'k6/metrics';

// Metrics
const wsConnections = new Gauge('ws_active_connections');
const peakConnections = new Counter('peak_connections');
const messagesSent = new Counter('messages_sent');
const messagesReceived = new Counter('messages_received');
const errors = new Counter('errors');
const httpLatency = new Trend('http_latency_ms');
const wsConnectLatency = new Trend('ws_connect_latency_ms');
const msgDeliveryLatency = new Trend('msg_delivery_latency_ms');
const successRate = new Rate('success_rate');

const SERVER_PORT = __ENV.PORT || '8001';
const TARGET_VUS = parseInt(__ENV.VUS) || 100;
const BASE_URL = `http://localhost:${SERVER_PORT}/api/v1`;
const WS_URL = `ws://localhost:${SERVER_PORT}/ws`;

// Each VU runs ONCE - simulating a real user that connects and stays
export const options = {
  scenarios: {
    concurrent_users: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '15s', target: TARGET_VUS },  // Ramp up
        { duration: '30s', target: TARGET_VUS },  // Hold
        { duration: '10s', target: 0 },           // Ramp down
      ],
      gracefulRampDown: '10s',
    },
  },
};

export default function () {
  const uniqueId = `${__VU}_${Date.now()}`;
  const username = `user_${uniqueId}`;

  // Step 1: Register
  const registerStart = Date.now();
  const registerRes = http.post(
    `${BASE_URL}/users/register`,
    JSON.stringify({
      username: username,
      email: `${username}@test.com`,
      password: 'test123',
    }),
    { headers: { 'Content-Type': 'application/json' } }
  );
  httpLatency.add(Date.now() - registerStart);

  if (registerRes.status !== 200) {
    errors.add(1);
    successRate.add(0);
    sleep(1);
    return;
  }

  const userData = JSON.parse(registerRes.body);
  const token = userData.token;

  // Step 2: Create room
  const createStart = Date.now();
  const createRoomRes = http.post(
    `${BASE_URL}/rooms`,
    JSON.stringify({ name: `room_${uniqueId}`, type: 'public' }),
    {
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${token}`,
      }
    }
  );
  httpLatency.add(Date.now() - createStart);

  if (createRoomRes.status !== 200) {
    errors.add(1);
    successRate.add(0);
    sleep(1);
    return;
  }

  const roomId = parseInt(JSON.parse(createRoomRes.body).id);

  // Step 3: Connect WebSocket and STAY CONNECTED
  const wsStart = Date.now();

  const res = ws.connect(WS_URL, {}, function (socket) {
    let authenticated = false;
    let joined = false;
    let msgSentTime = 0;

    socket.on('open', () => {
      wsConnectLatency.add(Date.now() - wsStart);
      wsConnections.add(1);
      peakConnections.add(1);
      socket.send(JSON.stringify({ type: 'auth', token: token }));
    });

    socket.on('message', (msg) => {
      const data = JSON.parse(msg);
      messagesReceived.add(1);

      if (data.type === 'success' && !authenticated) {
        authenticated = true;
        socket.send(JSON.stringify({ type: 'join_room', room_id: roomId }));
      }

      if (data.type === 'room_joined' && !joined) {
        joined = true;
        // Send messages periodically
        for (let i = 0; i < 3; i++) {
          msgSentTime = Date.now();
          socket.send(JSON.stringify({
            type: 'send_message',
            content: `Message ${i} from ${username}`,
          }));
          messagesSent.add(1);
        }
      }

      if (data.type === 'new_message' && msgSentTime > 0) {
        msgDeliveryLatency.add(Date.now() - msgSentTime);
      }
    });

    socket.on('close', () => {
      wsConnections.add(-1);
    });

    socket.on('error', () => {
      errors.add(1);
      wsConnections.add(-1);
    });

    // Stay connected for the duration of the test (30+ seconds)
    sleep(35);
  });

  if (check(res, { 'ws connected': (r) => r && r.status === 101 })) {
    successRate.add(1);
  } else {
    successRate.add(0);
    errors.add(1);
  }
}

export function handleSummary(data) {
  const get = (name, stat) => {
    if (data.metrics[name] && data.metrics[name].values) {
      return data.metrics[name].values[stat] || 0;
    }
    return 0;
  };

  const duration = Math.round(data.state.testRunDurationMs / 1000);

  const summary = `
╔══════════════════════════════════════════════════════════════════════╗
║         CONCURRENT USERS TEST - ${TARGET_VUS} USERS (commit 327f9cb)
╠══════════════════════════════════════════════════════════════════════╣
║ CONFIGURATION
║   Target Concurrent:    ${TARGET_VUS} users
║   Test Duration:        ${duration}s
║   Server:               localhost:${SERVER_PORT}
╠══════════════════════════════════════════════════════════════════════╣
║ CONNECTIONS
║   Peak Connections:     ${get('peak_connections', 'count')}
║   WS Sessions:          ${get('ws_sessions', 'count')}
╠══════════════════════════════════════════════════════════════════════╣
║ THROUGHPUT
║   HTTP Requests:        ${get('http_reqs', 'count')}
║   Messages Sent:        ${get('messages_sent', 'count')}
║   Messages Received:    ${get('messages_received', 'count')}
╠══════════════════════════════════════════════════════════════════════╣
║ HTTP LATENCY
║   Average:              ${Math.round(get('http_latency_ms', 'avg'))}ms
║   p50:                  ${Math.round(get('http_latency_ms', 'med'))}ms
║   p90:                  ${Math.round(get('http_latency_ms', 'p(90)'))}ms
║   p95:                  ${Math.round(get('http_latency_ms', 'p(95)'))}ms
║   p99:                  ${Math.round(get('http_latency_ms', 'p(99)'))}ms
╠══════════════════════════════════════════════════════════════════════╣
║ WEBSOCKET CONNECT LATENCY
║   Average:              ${Math.round(get('ws_connect_latency_ms', 'avg'))}ms
║   p95:                  ${Math.round(get('ws_connect_latency_ms', 'p(95)'))}ms
╠══════════════════════════════════════════════════════════════════════╣
║ MESSAGE DELIVERY LATENCY
║   Average:              ${Math.round(get('msg_delivery_latency_ms', 'avg'))}ms
║   p95:                  ${Math.round(get('msg_delivery_latency_ms', 'p(95)'))}ms
║   p99:                  ${Math.round(get('msg_delivery_latency_ms', 'p(99)'))}ms
╠══════════════════════════════════════════════════════════════════════╣
║ SUCCESS/ERRORS
║   Success Rate:         ${(get('success_rate', 'rate') * 100).toFixed(1)}%
║   Errors:               ${get('errors', 'count')}
╚══════════════════════════════════════════════════════════════════════╝
`;
  console.log(summary);
  return { 'stdout': summary };
}

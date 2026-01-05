import http from 'k6/http';
import ws from 'k6/ws';
import { check, sleep } from 'k6';
import { Counter, Gauge, Trend, Rate } from 'k6/metrics';

// Custom metrics - Connections
const wsConnections = new Gauge('ws_active_connections');
const peakConnections = new Gauge('peak_connections');
const totalConnections = new Counter('total_connections');

// Custom metrics - Messages
const messagesSent = new Counter('messages_sent_total');
const messagesReceived = new Counter('messages_received_total');

// Custom metrics - Errors
const errors = new Counter('errors_total');
const wsErrors = new Counter('ws_errors');
const httpErrors = new Counter('http_errors');

// Custom metrics - Latency (Trends give us percentiles)
const wsConnectTime = new Trend('ws_connect_latency_ms');
const httpRegisterTime = new Trend('http_register_latency_ms');
const httpCreateRoomTime = new Trend('http_create_room_latency_ms');
const messageDeliveryTime = new Trend('message_delivery_latency_ms');
const e2eLatency = new Trend('e2e_latency_ms'); // End-to-end: register -> message sent

// Custom metrics - Success rates
const successRate = new Rate('success_rate');
const wsSuccessRate = new Rate('ws_success_rate');
const httpSuccessRate = new Rate('http_success_rate');

const SERVER_PORT = __ENV.PORT || '8001';
const TARGET_VUS = parseInt(__ENV.VUS) || 100;
const BASE_URL = `http://localhost:${SERVER_PORT}/api/v1`;
const WS_URL = `ws://localhost:${SERVER_PORT}/ws`;

export const options = {
  stages: [
    { duration: '10s', target: Math.floor(TARGET_VUS * 0.5) },  // Ramp to 50%
    { duration: '10s', target: TARGET_VUS },                     // Ramp to 100%
    { duration: '20s', target: TARGET_VUS },                     // Hold at target
    { duration: '10s', target: 0 },                              // Ramp down
  ],
  thresholds: {
    'http_req_duration': ['p(95)<2000'],
    'errors_total': ['count<100'],
  },
};

export default function () {
  const e2eStart = Date.now();
  const uniqueId = `${__VU}_${__ITER}_${Date.now()}`;
  const username = `user_${uniqueId}`;

  // Step 1: Register user
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
  httpRegisterTime.add(Date.now() - registerStart);

  if (registerRes.status !== 200) {
    errors.add(1);
    httpErrors.add(1);
    successRate.add(0);
    httpSuccessRate.add(0);
    return;
  }
  httpSuccessRate.add(1);

  const userData = JSON.parse(registerRes.body);
  const token = userData.token;

  // Step 2: Create a room
  const createRoomStart = Date.now();
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
  httpCreateRoomTime.add(Date.now() - createRoomStart);

  let roomId = 1;
  if (createRoomRes.status === 200) {
    const roomData = JSON.parse(createRoomRes.body);
    roomId = parseInt(roomData.id);
    httpSuccessRate.add(1);
  } else {
    errors.add(1);
    httpErrors.add(1);
    successRate.add(0);
    httpSuccessRate.add(0);
    return;
  }

  // Step 3: Connect WebSocket
  const wsConnectStart = Date.now();
  let wsConnected = false;
  let messageSentTime = 0;
  let messageDelivered = false;

  const res = ws.connect(WS_URL, {}, function (socket) {
    let authenticated = false;
    let roomJoined = false;

    socket.on('open', () => {
      wsConnectTime.add(Date.now() - wsConnectStart);
      wsConnections.add(1);
      totalConnections.add(1);
      wsConnected = true;
      socket.send(JSON.stringify({ type: 'auth', token: token }));
    });

    socket.on('message', (msg) => {
      const data = JSON.parse(msg);
      messagesReceived.add(1);

      if (data.type === 'success' && !authenticated) {
        authenticated = true;
        socket.send(JSON.stringify({
          type: 'join_room',
          room_id: roomId,
        }));
      }

      if (data.type === 'room_joined' && !roomJoined) {
        roomJoined = true;
        messageSentTime = Date.now();
        messagesSent.add(1);
        socket.send(JSON.stringify({
          type: 'send_message',
          content: `Hello from ${username}`,
        }));
      }

      // Track message delivery time when we receive our own message back
      if (data.type === 'new_message' && messageSentTime > 0 && !messageDelivered) {
        messageDeliveryTime.add(Date.now() - messageSentTime);
        e2eLatency.add(Date.now() - e2eStart);
        messageDelivered = true;
        messageSentTime = 0;
      }
    });

    socket.on('close', () => {
      wsConnections.add(-1);
    });

    socket.on('error', (e) => {
      wsErrors.add(1);
      errors.add(1);
      wsConnections.add(-1);
    });

    // Stay connected briefly then close
    socket.setTimeout(() => {
      socket.close();
    }, 2000);
  });

  const wsOk = check(res, { 'ws connected': (r) => r && r.status === 101 });

  if (wsOk && wsConnected) {
    successRate.add(1);
    wsSuccessRate.add(1);
  } else {
    successRate.add(0);
    wsSuccessRate.add(0);
    errors.add(1);
  }

  sleep(0.5);
}

export function handleSummary(data) {
  const getMetric = (name, stat) => {
    if (data.metrics[name] && data.metrics[name].values) {
      return data.metrics[name].values[stat] || 0;
    }
    return 0;
  };

  const duration = data.state.testRunDurationMs / 1000;
  const totalReqs = getMetric('http_reqs', 'count');
  const throughput = (totalReqs / duration).toFixed(1);

  const summary = `
╔══════════════════════════════════════════════════════════════════════╗
║              LOAD TEST RESULTS - ${TARGET_VUS} CONCURRENT USERS
╠══════════════════════════════════════════════════════════════════════╣
║ TEST CONFIGURATION
║   Target Users:         ${TARGET_VUS}
║   Test Duration:        ${Math.round(duration)}s
║   Server:               localhost:${SERVER_PORT}
╠══════════════════════════════════════════════════════════════════════╣
║ THROUGHPUT
║   HTTP Requests:        ${totalReqs}
║   Requests/sec:         ${throughput}
║   Messages Sent:        ${getMetric('messages_sent_total', 'count')}
║   Messages Received:    ${getMetric('messages_received_total', 'count')}
╠══════════════════════════════════════════════════════════════════════╣
║ HTTP LATENCY (Register + Create Room)
║   Average:              ${Math.round(getMetric('http_req_duration', 'avg'))}ms
║   Median (p50):         ${Math.round(getMetric('http_req_duration', 'med'))}ms
║   p90:                  ${Math.round(getMetric('http_req_duration', 'p(90)'))}ms
║   p95:                  ${Math.round(getMetric('http_req_duration', 'p(95)'))}ms
║   p99:                  ${Math.round(getMetric('http_req_duration', 'p(99)'))}ms
║   Min:                  ${Math.round(getMetric('http_req_duration', 'min'))}ms
║   Max:                  ${Math.round(getMetric('http_req_duration', 'max'))}ms
╠══════════════════════════════════════════════════════════════════════╣
║ REGISTRATION API LATENCY
║   Average:              ${Math.round(getMetric('http_register_latency_ms', 'avg'))}ms
║   p95:                  ${Math.round(getMetric('http_register_latency_ms', 'p(95)'))}ms
║   p99:                  ${Math.round(getMetric('http_register_latency_ms', 'p(99)'))}ms
╠══════════════════════════════════════════════════════════════════════╣
║ CREATE ROOM API LATENCY
║   Average:              ${Math.round(getMetric('http_create_room_latency_ms', 'avg'))}ms
║   p95:                  ${Math.round(getMetric('http_create_room_latency_ms', 'p(95)'))}ms
║   p99:                  ${Math.round(getMetric('http_create_room_latency_ms', 'p(99)'))}ms
╠══════════════════════════════════════════════════════════════════════╣
║ WEBSOCKET METRICS
║   Total Connections:    ${getMetric('total_connections', 'count')}
║   WS Sessions:          ${getMetric('ws_sessions', 'count')}
║   Connect Latency Avg:  ${Math.round(getMetric('ws_connect_latency_ms', 'avg'))}ms
║   Connect Latency p95:  ${Math.round(getMetric('ws_connect_latency_ms', 'p(95)'))}ms
║   Connect Latency p99:  ${Math.round(getMetric('ws_connect_latency_ms', 'p(99)'))}ms
╠══════════════════════════════════════════════════════════════════════╣
║ MESSAGE DELIVERY LATENCY (WebSocket round-trip)
║   Average:              ${Math.round(getMetric('message_delivery_latency_ms', 'avg'))}ms
║   p95:                  ${Math.round(getMetric('message_delivery_latency_ms', 'p(95)'))}ms
║   p99:                  ${Math.round(getMetric('message_delivery_latency_ms', 'p(99)'))}ms
╠══════════════════════════════════════════════════════════════════════╣
║ END-TO-END LATENCY (Register → Message Delivered)
║   Average:              ${Math.round(getMetric('e2e_latency_ms', 'avg'))}ms
║   p95:                  ${Math.round(getMetric('e2e_latency_ms', 'p(95)'))}ms
║   p99:                  ${Math.round(getMetric('e2e_latency_ms', 'p(99)'))}ms
╠══════════════════════════════════════════════════════════════════════╣
║ SUCCESS RATES
║   Overall:              ${(getMetric('success_rate', 'rate') * 100).toFixed(1)}%
║   HTTP:                 ${(getMetric('http_success_rate', 'rate') * 100).toFixed(1)}%
║   WebSocket:            ${(getMetric('ws_success_rate', 'rate') * 100).toFixed(1)}%
╠══════════════════════════════════════════════════════════════════════╣
║ ERRORS
║   Total:                ${getMetric('errors_total', 'count')}
║   HTTP Errors:          ${getMetric('http_errors', 'count')}
║   WebSocket Errors:     ${getMetric('ws_errors', 'count')}
╚══════════════════════════════════════════════════════════════════════╝
`;

  console.log(summary);
  return { 'stdout': summary };
}

import ws from 'k6/ws';
import { check, sleep } from 'k6';
import http from 'k6/http';
import { Counter, Gauge, Trend } from 'k6/metrics';

// Custom metrics
const activeConnections = new Gauge('active_connections');
const messagesSent = new Counter('messages_sent');
const messagesReceived = new Counter('messages_received');
const errors = new Counter('errors');
const wsConnectTime = new Trend('ws_connect_time_ms');
const messageLatency = new Trend('message_latency_ms');
const authLatency = new Trend('auth_latency_ms');

// Test: Ramp up to 50,000 concurrent connections
export const options = {
  scenarios: {
    scale_test: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '30s', target: 5000 },   // Warm up to 5K
        { duration: '30s', target: 10000 },  // 10K
        { duration: '30s', target: 20000 },  // 20K
        { duration: '30s', target: 30000 },  // 30K
        { duration: '30s', target: 40000 },  // 40K
        { duration: '30s', target: 50000 },  // 50K
        { duration: '2m', target: 50000 },   // Hold 50K for 2 min
        { duration: '30s', target: 0 },      // Ramp down
      ],
    },
  },
};

// AWS endpoint
const BASE_URL = 'http://54.79.163.178/api/v1';
const WS_URL = 'ws://54.79.163.178/ws';

// Pre-shared test token
const TEST_TOKEN = __ENV.TOKEN || '';

export function setup() {
  if (TEST_TOKEN) {
    console.log('Using provided token');
    return { token: TEST_TOKEN };
  }

  const email = 'loadtest50k@test.com';
  const password = 'test123';

  http.post(`${BASE_URL}/users/register`, JSON.stringify({
    email: email,
    username: 'loadtest50k',
    password: password,
  }), { headers: { 'Content-Type': 'application/json' } });

  const loginRes = http.post(`${BASE_URL}/users/login`, JSON.stringify({
    email: email,
    password: password,
  }), { headers: { 'Content-Type': 'application/json' } });

  if (loginRes.status !== 200) {
    console.error('Failed to get test token');
    return { token: '' };
  }

  const token = JSON.parse(loginRes.body).token;
  console.log('Got test token for all VUs');
  return { token: token };
}

export default function (data) {
  const token = data.token;
  if (!token) {
    errors.add(1);
    return;
  }

  const userId = __VU;

  const wsStart = Date.now();
  const res = ws.connect(WS_URL, {}, function (socket) {
    let authStart = 0;
    let msgSendTime = 0;

    socket.on('open', () => {
      wsConnectTime.add(Date.now() - wsStart);
      activeConnections.add(1);

      authStart = Date.now();
      socket.send(JSON.stringify({ type: 'auth', token: token }));
    });

    socket.on('message', (data) => {
      const msg = JSON.parse(data);
      messagesReceived.add(1);

      if (msg.type === 'success' && msg.message.includes('Authenticated')) {
        authLatency.add(Date.now() - authStart);
        socket.send(JSON.stringify({ type: 'join_room', room_id: 1 }));
      }

      if (msg.type === 'room_joined') {
        msgSendTime = Date.now();
        socket.send(JSON.stringify({
          type: 'send_message',
          content: `Hello from user ${userId} at ${msgSendTime}`,
        }));
        messagesSent.add(1);
      }

      if (msg.type === 'new_message' && msgSendTime > 0) {
        messageLatency.add(Date.now() - msgSendTime);
        msgSendTime = 0;
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
  const duration = data.state.testRunDurationMs / 1000;
  const msgSent = data.metrics.messages_sent?.values?.count || 0;
  const msgRecv = data.metrics.messages_received?.values?.count || 0;
  const httpReqs = data.metrics.http_reqs?.values?.count || 0;
  const errCount = data.metrics.errors?.values?.count || 0;
  const peakConn = data.metrics.active_connections?.values?.max || 0;

  const summary = `
╔═══════════════════════════════════════════════════════════════╗
║           AWS LOAD TEST RESULTS (Target: 50,000 VUs)          ║
╠═══════════════════════════════════════════════════════════════╣
║ CONNECTIONS                                                   ║
╟───────────────────────────────────────────────────────────────╢
║  Peak Concurrent:     ${String(peakConn).padStart(8)}                            ║
║  Total HTTP Requests: ${String(httpReqs).padStart(8)}                            ║
║  Errors:              ${String(errCount).padStart(8)}                            ║
╠═══════════════════════════════════════════════════════════════╣
║ THROUGHPUT                                                    ║
╟───────────────────────────────────────────────────────────────╢
║  HTTP Requests/sec:   ${String((httpReqs / duration).toFixed(1)).padStart(8)}                            ║
║  Messages Sent:       ${String(msgSent).padStart(8)}                            ║
║  Messages Received:   ${String(msgRecv).padStart(8)}                            ║
║  Msg Throughput/sec:  ${String((msgSent / duration).toFixed(1)).padStart(8)}                            ║
╠═══════════════════════════════════════════════════════════════╣
║ LATENCY                                                       ║
╟───────────────────────────────────────────────────────────────╢
║  HTTP Request:                                                ║
║    avg: ${String((data.metrics.http_req_duration?.values?.avg || 0).toFixed(0)).padStart(6)}ms    p95: ${String((data.metrics.http_req_duration?.values['p(95)'] || 0).toFixed(0)).padStart(6)}ms    p99: ${String((data.metrics.http_req_duration?.values['p(99)'] || 0).toFixed(0)).padStart(6)}ms  ║
║  WebSocket Connect:                                           ║
║    avg: ${String((data.metrics.ws_connect_time_ms?.values?.avg || 0).toFixed(0)).padStart(6)}ms    p95: ${String((data.metrics.ws_connect_time_ms?.values['p(95)'] || 0).toFixed(0)).padStart(6)}ms    p99: ${String((data.metrics.ws_connect_time_ms?.values['p(99)'] || 0).toFixed(0)).padStart(6)}ms  ║
║  Auth (JWT validate):                                         ║
║    avg: ${String((data.metrics.auth_latency_ms?.values?.avg || 0).toFixed(0)).padStart(6)}ms    p95: ${String((data.metrics.auth_latency_ms?.values['p(95)'] || 0).toFixed(0)).padStart(6)}ms    p99: ${String((data.metrics.auth_latency_ms?.values['p(99)'] || 0).toFixed(0)).padStart(6)}ms  ║
║  Message (end-to-end):                                        ║
║    avg: ${String((data.metrics.message_latency_ms?.values?.avg || 0).toFixed(0)).padStart(6)}ms    p95: ${String((data.metrics.message_latency_ms?.values['p(95)'] || 0).toFixed(0)).padStart(6)}ms    p99: ${String((data.metrics.message_latency_ms?.values['p(99)'] || 0).toFixed(0)).padStart(6)}ms  ║
╠═══════════════════════════════════════════════════════════════╣
║ TEST INFO                                                     ║
╟───────────────────────────────────────────────────────────────╢
║  Duration:            ${String((duration / 60).toFixed(1)).padStart(6)} minutes                        ║
║  Target:              http://54.79.163.178                    ║
╚═══════════════════════════════════════════════════════════════╝
`;
  console.log(summary);
  return { 'stdout': summary };
}

import http from 'k6/http';
import ws from 'k6/ws';
import { check, sleep } from 'k6';
import { Trend, Counter, Gauge, Rate } from 'k6/metrics';

// ============================================
// COMPREHENSIVE CHAT APP LOAD TEST
// Tests ALL important metrics:
// 1. Message delivery latency
// 2. Concurrent connections
// 3. Messages per second (throughput)
// 4. Connection success rate
// 5. Error rate
// 6. Cross-server delivery (via Redis Pub/Sub)
// ============================================

// Custom Metrics
const messageLatency = new Trend('message_latency_ms');           // How fast messages deliver
const connectionTime = new Trend('websocket_connection_time_ms'); // Time to establish WebSocket
const activeConnections = new Gauge('concurrent_connections');     // Current connected users
const messagesSent = new Counter('messages_sent_total');          // Total messages sent
const messagesReceived = new Counter('messages_received_total');  // Total messages received
const connectionSuccess = new Rate('connection_success_rate');    // % successful connections
const messageSuccess = new Rate('message_delivery_rate');         // % messages delivered
const errors = new Counter('errors_total');                       // Total errors

// Server config - can test different servers
const SERVER_PORT = __ENV.PORT || '8001';
const BASE_URL = `http://localhost:${SERVER_PORT}/api/v1`;
const WS_URL = `ws://localhost:${SERVER_PORT}/ws`;

// Test configuration
export const options = {
  scenarios: {
    load_test: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '30s', target: 50 },    // Warm up
        { duration: '30s', target: 100 },   // Ramp to 100
        { duration: '1m', target: 200 },    // Ramp to 200
        { duration: '2m', target: 200 },    // HOLD - measure steady state
        { duration: '30s', target: 0 },     // Ramp down
      ],
    },
  },
  thresholds: {
    'message_latency_ms': ['p(95)<500'],      // 95% of messages under 500ms
    'connection_success_rate': ['rate>0.95'], // 95% connections succeed
    'message_delivery_rate': ['rate>0.99'],   // 99% messages delivered
    'errors_total': ['count<50'],             // Less than 50 errors
  },
};

export default function () {
  const uniqueId = `${SERVER_PORT}_${__VU}_${__ITER}_${Date.now()}`;
  const username = `user_${uniqueId}`;

  // ============ STEP 1: Register ============
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

  const registerOk = check(registerRes, {
    'registration successful': (r) => r.status === 200,
  });

  if (!registerOk) {
    errors.add(1);
    connectionSuccess.add(0);
    return;
  }

  const token = JSON.parse(registerRes.body).token;

  // ============ STEP 2: Join Room via API ============
  const joinRoomRes = http.post(
    `${BASE_URL}/rooms/1/join`,
    '{}',
    { headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${token}` } }
  );

  const joinOk = check(joinRoomRes, {
    'joined room via API': (r) => r.status === 200,
  });

  if (!joinOk) {
    errors.add(1);
    return;
  }

  // ============ STEP 3: WebSocket Connection ============
  const wsConnectStart = Date.now();
  let connectionEstablished = false;
  let authenticated = false;
  let inRoom = false;
  let myMessageCount = 0;
  let receivedOwnMessages = 0;

  const res = ws.connect(WS_URL, {}, function (socket) {
    socket.on('open', () => {
      const connectTime = Date.now() - wsConnectStart;
      connectionTime.add(connectTime);
      connectionEstablished = true;
      activeConnections.add(1);
      connectionSuccess.add(1);

      // Authenticate immediately
      socket.send(JSON.stringify({ type: 'auth', token: token }));
    });

    socket.on('message', (data) => {
      const msg = JSON.parse(data);

      // Handle authentication success
      if (msg.type === 'success' && !authenticated) {
        authenticated = true;
        // Join room 1
        socket.send(JSON.stringify({ type: 'join_room', room_id: 1 }));
      }

      // Handle room joined
      if (msg.type === 'room_joined' && !inRoom) {
        inRoom = true;
        // Start sending messages
        startSendingMessages(socket, username);
      }

      // Handle received messages - measure latency
      if (msg.type === 'new_message') {
        messagesReceived.add(1);

        // Check if this is OUR message (measure round-trip latency)
        const content = msg.content || '';
        if (content.includes(`[${username}]`) && content.includes('sent:')) {
          receivedOwnMessages++;
          messageSuccess.add(1);

          // Extract timestamp and calculate latency
          const match = content.match(/sent:(\d+)/);
          if (match) {
            const sentTime = parseInt(match[1]);
            const latency = Date.now() - sentTime;
            messageLatency.add(latency);
          }
        }
      }

      // Handle errors
      if (msg.type === 'error') {
        errors.add(1);
        messageSuccess.add(0);
      }
    });

    socket.on('close', () => {
      activeConnections.add(-1);
    });

    socket.on('error', (e) => {
      errors.add(1);
      connectionSuccess.add(0);
    });

    // Keep connection alive for 60 seconds
    socket.setTimeout(() => {
      socket.close();
    }, 60000);

    // Heartbeat every 25 seconds
    socket.setInterval(() => {
      socket.send(JSON.stringify({ type: 'ping' }));
    }, 25000);

    // Helper function to send messages
    function startSendingMessages(socket, username) {
      let count = 0;
      const maxMessages = 5; // Each user sends 5 messages

      const sendMessage = () => {
        if (count >= maxMessages) return;

        const timestamp = Date.now();
        const content = `[${username}] Message #${count} sent:${timestamp}`;

        socket.send(JSON.stringify({
          type: 'send_message',
          room_id: 1,
          content: content,
        }));

        messagesSent.add(1);
        myMessageCount++;
        count++;

        // Send next message after 5-10 seconds (realistic interval)
        socket.setTimeout(sendMessage, 5000 + Math.random() * 5000);
      };

      // Start after 2 seconds
      socket.setTimeout(sendMessage, 2000);
    }
  });

  // Check WebSocket connection
  check(res, {
    'websocket connected': (r) => r && r.status === 101,
  });

  // Wait for session to complete
  sleep(60);
}

// ============ SUMMARY REPORT ============
export function handleSummary(data) {
  const m = data.metrics;

  const report = `
================================================================================
                    COMPREHENSIVE CHAT APP LOAD TEST RESULTS
================================================================================

SERVER TESTED: localhost:${SERVER_PORT}
TEST DURATION: ${Math.round(data.state.testRunDurationMs / 1000)} seconds

--------------------------------------------------------------------------------
1. MESSAGE DELIVERY LATENCY (How fast messages arrive)
--------------------------------------------------------------------------------
   Average:    ${m.message_latency_ms?.values?.avg?.toFixed(2) || 'N/A'} ms
   Median:     ${m.message_latency_ms?.values?.med?.toFixed(2) || 'N/A'} ms
   p90:        ${m.message_latency_ms?.values['p(90)']?.toFixed(2) || 'N/A'} ms
   p95:        ${m.message_latency_ms?.values['p(95)']?.toFixed(2) || 'N/A'} ms
   Max:        ${m.message_latency_ms?.values?.max?.toFixed(2) || 'N/A'} ms

   TARGET: p95 < 500ms  ${(m.message_latency_ms?.values['p(95)'] || 0) < 500 ? '✅ PASS' : '❌ FAIL'}

--------------------------------------------------------------------------------
2. CONCURRENT CONNECTIONS (How many users at once)
--------------------------------------------------------------------------------
   Peak:       ${m.concurrent_connections?.values?.max || 0} users

--------------------------------------------------------------------------------
3. THROUGHPUT (Messages per second)
--------------------------------------------------------------------------------
   Sent:       ${m.messages_sent_total?.values?.count || 0} messages
   Received:   ${m.messages_received_total?.values?.count || 0} messages
   Rate:       ${((m.messages_sent_total?.values?.count || 0) / (data.state.testRunDurationMs / 1000)).toFixed(2)} msg/sec

--------------------------------------------------------------------------------
4. CONNECTION SUCCESS RATE
--------------------------------------------------------------------------------
   Rate:       ${((m.connection_success_rate?.values?.rate || 0) * 100).toFixed(2)}%

   TARGET: > 95%  ${(m.connection_success_rate?.values?.rate || 0) > 0.95 ? '✅ PASS' : '❌ FAIL'}

--------------------------------------------------------------------------------
5. MESSAGE DELIVERY RATE
--------------------------------------------------------------------------------
   Rate:       ${((m.message_delivery_rate?.values?.rate || 0) * 100).toFixed(2)}%

   TARGET: > 99%  ${(m.message_delivery_rate?.values?.rate || 0) > 0.99 ? '✅ PASS' : '❌ FAIL'}

--------------------------------------------------------------------------------
6. ERRORS
--------------------------------------------------------------------------------
   Total:      ${m.errors_total?.values?.count || 0}

   TARGET: < 50  ${(m.errors_total?.values?.count || 0) < 50 ? '✅ PASS' : '❌ FAIL'}

--------------------------------------------------------------------------------
WEBSOCKET DETAILS
--------------------------------------------------------------------------------
   Connection Time (avg):  ${m.websocket_connection_time_ms?.values?.avg?.toFixed(2) || 'N/A'} ms
   Connection Time (p95):  ${m.websocket_connection_time_ms?.values['p(95)']?.toFixed(2) || 'N/A'} ms

================================================================================
                              OVERALL RESULT
================================================================================
${getOverallResult(m)}
================================================================================
`;

  console.log(report);

  return {
    'stdout': report,
    'comprehensive-test-results.txt': report,
  };
}

function getOverallResult(m) {
  const latencyOk = (m.message_latency_ms?.values['p(95)'] || 0) < 500;
  const connectionOk = (m.connection_success_rate?.values?.rate || 0) > 0.95;
  const deliveryOk = (m.message_delivery_rate?.values?.rate || 0) > 0.99;
  const errorsOk = (m.errors_total?.values?.count || 0) < 50;

  const passed = [latencyOk, connectionOk, deliveryOk, errorsOk].filter(x => x).length;

  if (passed === 4) {
    return '   ✅ ALL TESTS PASSED - Your chat app is production ready!';
  } else if (passed >= 2) {
    return `   ⚠️  ${passed}/4 TESTS PASSED - Some improvements needed`;
  } else {
    return `   ❌ ${passed}/4 TESTS PASSED - Significant issues detected`;
  }
}

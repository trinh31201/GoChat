import http from 'k6/http';
import { check, sleep } from 'k6';

// This script pre-creates test users in the database
// Run ONCE before the concurrent connection test

export const options = {
  scenarios: {
    create_users: {
      executor: 'shared-iterations',
      vus: 50, // 50 parallel workers
      iterations: 2000, // Create 2000 users total
      maxDuration: '5m',
    },
  },
};

const BASE_URL = 'http://localhost/api/v1';

export default function () {
  // Use iteration number for unique users
  const userId = __ITER;
  const username = `testuser${userId}`;
  const email = `testuser${userId}@loadtest.com`;
  const password = 'testpass123';

  // Register user
  const registerRes = http.post(
    `${BASE_URL}/users/register`,
    JSON.stringify({
      username: username,
      email: email,
      password: password,
    }),
    { headers: { 'Content-Type': 'application/json' } }
  );

  const success = check(registerRes, {
    'user created': (r) => r.status === 200,
  });

  if (success) {
    const userData = JSON.parse(registerRes.body);
    const token = userData.token;
    const userId = userData.user.id;

    // Join room 1 (main test room)
    const joinRes = http.post(
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

    check(joinRes, {
      'joined room': (r) => r.status === 200
    });
  }

  sleep(0.1); // Small delay to avoid overwhelming DB
}

export function handleSummary(data) {
  return {
    'stdout': textSummary(data, { indent: ' ', enableColors: true }),
  };
}

function textSummary(data, options) {
  const summary = `
========================================
Test User Creation Summary
========================================

Users created: ${data.metrics.iterations.values.count}
Success rate:  ${data.metrics.checks.values.passes / data.metrics.checks.values.count * 100}%
Duration:      ${data.state.testRunDurationMs / 1000}s

These users can now be used for load testing:
- Username: testuser0 to testuser${data.metrics.iterations.values.count - 1}
- Password: testpass123
- All joined room 1

Next: Run the concurrent connection test!
========================================
`;
  return summary;
}

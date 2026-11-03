import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  stages: [
    { duration: '10s', target: 50 },  // Ramp up to 50 users
    { duration: '30s', target: 200 }, // Spike to 200 concurrent users
    { duration: '10s', target: 0 },   // Ramp down
  ],
};

export default function () {
  // Test Shorten Endpoint
  const payload = JSON.stringify({ url: 'https://github.com' });
  const params = { headers: { 'Content-Type': 'application/json' } };
  
  const res = http.post('http://localhost:8080/api/v1/shorten', payload, params);
  check(res, { 'status was 201': (r) => r.status === 201 });

  sleep(1);
}

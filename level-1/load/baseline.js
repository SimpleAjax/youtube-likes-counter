import http from 'k6/http';
import { check } from 'k6';

const videoIds = [
  '11111111-1111-4111-8111-111111111111',
  '22222222-2222-4222-8222-222222222222',
  '33333333-3333-4333-8333-333333333333',
  '44444444-4444-4444-8444-444444444444',
  '55555555-5555-4555-8555-555555555555',
];

const rate = Number(__ENV.RATE || 5);
const duration = __ENV.DURATION || '2m';
const writePercent = Number(__ENV.WRITE_PERCENT || 10);
const videoMode = __ENV.VIDEO_MODE || 'hot';
const baseUrl = __ENV.API_URL || 'http://localhost:8080';

export const options = {
  scenarios: {
    baseline: {
      executor: 'constant-arrival-rate',
      rate,
      timeUnit: '1s',
      duration,
      preAllocatedVUs: Math.max(20, Math.ceil(rate / 2)),
      maxVUs: Math.max(100, rate * 2),
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.05'],
  },
};

export default function () {
  const videoId = videoMode === 'spread' ? videoIds[Math.floor(Math.random() * videoIds.length)] : videoIds[0];
  const viewerId = crypto.randomUUID();
  const params = { headers: { 'X-Demo-Viewer-Id': viewerId } };
  const url = `${baseUrl}/api/videos/${videoId}`;
  const response = Math.random() * 100 < writePercent
    ? http.put(`${url}/like`, null, params)
    : http.get(url, params);
  check(response, { 'HTTP 200': (result) => result.status === 200 });
}

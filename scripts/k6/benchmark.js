import http from 'k6/http';
import { check } from 'k6';

export const options = {
  scenarios: {
    gifts_load: {
      executor: 'constant-arrival-rate',
      rate: 1000,
      timeUnit: '1s',
      duration: '10m',
      preAllocatedVUs: 200,
      maxVUs: 1200,
    },
  },
  thresholds: {
    http_req_duration: ['p(95)<75', 'p(99)<150'],
    http_req_failed: ['rate<0.001'],
  },
};

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

export default function () {
  const id = `${__VU}-${__ITER}`;
  const dup = __ITER % 50 === 0;
  const lowBalance = __ITER % 100 === 0;
  const reqID = dup ? `dup-${Math.floor(__ITER / 50)}` : `gift-${id}`;
  const viewerID = lowBalance ? 2002 : 2001;

  const body = JSON.stringify({
    request_id: reqID,
    viewer_id: viewerID,
    creator_id: 1001,
    live_session_id: 9001,
    match_id: 8001,
    gift_id: lowBalance ? 'LION' : 'ROSE',
    quantity: 1,
    sent_at_ms: Date.now(),
  });

  const res = http.post(`${BASE_URL}/v1/gifts`, body, {
    headers: { 'Content-Type': 'application/json' },
    responseCallback: http.expectedStatuses(201, 409, 403, 404),
  });

  check(res, {
    'gift expected status': (r) => [201, 409, 403, 404].includes(r.status),
  });
}

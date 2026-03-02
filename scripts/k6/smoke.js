import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  vus: 10,
  duration: '30s',
  thresholds: {
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(95)<200'],
  },
};

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

export default function () {
  const rID = `k6-smoke-${__VU}-${__ITER}`;
  const rechargeBody = JSON.stringify({
    request_id: `recharge-${rID}`,
    viewer_id: 2001,
    coins: 10,
    payment_ref: `pay-${rID}`,
  });
  const rechargeRes = http.post(`${BASE_URL}/v1/wallets/recharges`, rechargeBody, {
    headers: { 'Content-Type': 'application/json' },
    responseCallback: http.expectedStatuses(201),
  });
  check(rechargeRes, {
    'recharge status 201': (r) => r.status === 201,
  });

  const giftBody = JSON.stringify({
    request_id: `gift-${rID}`,
    viewer_id: 2001,
    creator_id: 1001,
    live_session_id: 9001,
    gift_id: 'ROSE',
    quantity: 1,
    sent_at_ms: Date.now(),
  });
  const giftRes = http.post(`${BASE_URL}/v1/gifts`, giftBody, {
    headers: { 'Content-Type': 'application/json' },
    responseCallback: http.expectedStatuses(201, 409),
  });
  check(giftRes, {
    'gift status 201 or 409': (r) => r.status === 201 || r.status === 409,
  });

  sleep(0.1);
}

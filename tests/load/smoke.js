import http from 'k6/http';
import { check, group } from 'k6';

const apiURL = __ENV.API_URL || 'http://host.docker.internal:8080';
const tokenURL = __ENV.OIDC_TOKEN_URL || 'http://host.docker.internal:8081/realms/fccp/protocol/openid-connect/token';

export const options = {
  scenarios: {
    authenticated_reads: {
      executor: 'constant-arrival-rate',
      rate: Number(__ENV.FCCP_RATE || 10),
      timeUnit: '1s',
      duration: __ENV.FCCP_DURATION || '1m',
      preAllocatedVUs: 10,
      maxVUs: 50,
    },
  },
  thresholds: {
    checks: ['rate>0.99'],
    'http_req_failed{workload:authenticated-read}': ['rate<0.01'],
    'http_req_duration{workload:authenticated-read}': ['p(95)<500'],
  },
};

export function setup() {
  const response = http.post(tokenURL, {
    grant_type: 'client_credentials',
    client_id: __ENV.OIDC_CLIENT_ID || 'fccp-load-test',
    client_secret: __ENV.OIDC_CLIENT_SECRET || 'load-test-demo-only',
  });
  check(response, {'service account authenticated': (result) => result.status === 200 && Boolean(result.json('access_token'))});
  if (response.status !== 200) throw new Error(`token request failed: ${response.status}`);
  return {token: response.json('access_token')};
}

export default function (data) {
  const params = {headers: {Authorization: `Bearer ${data.token}`}, tags: {workload: 'authenticated-read'}};
  group('operator dashboard reads', () => {
    const responses = http.batch([
      ['GET', `${apiURL}/v1/customers?page_size=20`, null, params],
      ['GET', `${apiURL}/v1/alerts?page_size=20`, null, params],
      ['GET', `${apiURL}/v1/notifications`, null, params],
      ['GET', `${apiURL}/v1/notification-preferences`, null, params],
    ]);
    if (__ITER === 0 && responses.some((response) => response.status !== 200)) {
      console.error(`dashboard read statuses: ${responses.map((response) => response.status).join(',')}`);
    }
    responses.forEach((response) => check(response, {'authenticated read succeeded': (result) => result.status === 200}));
  });
}

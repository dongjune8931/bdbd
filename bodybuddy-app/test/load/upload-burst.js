import http from "k6/http";
import { check, sleep } from "k6";
import { Counter, Rate } from "k6/metrics";

const baseUrl = __ENV.K6_BASE_URL || "http://localhost:8080";
const token = __ENV.K6_TOKEN;
const thinkTimeMs = Number(__ENV.K6_THINK_TIME_MS || "250");

if (!token) {
  throw new Error("K6_TOKEN is required");
}

export const uploadQueued = new Counter("bodybuddy_upload_queued_total");
export const uploadFailed = new Counter("bodybuddy_upload_failed_total");
export const uploadFailureRate = new Rate("bodybuddy_upload_failure_rate");

export const options = {
  stages: [
    { duration: "30s", target: 5 },
    { duration: "1m", target: 15 },
    { duration: "1m", target: 30 },
    { duration: "30s", target: 0 },
  ],
  thresholds: {
    http_req_failed: ["rate<0.05"],
    http_req_duration: ["p(95)<1500"],
    bodybuddy_upload_failure_rate: ["rate<0.05"],
  },
};

function buildPayload() {
  const unique = `${__VU}-${__ITER}-${Date.now()}`;
  return JSON.stringify({
    filename: `load-upload-${unique}.jpg`,
  });
}

export default function () {
  const response = http.post(`${baseUrl}/api/v1/uploads`, buildPayload(), {
    headers: {
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json",
    },
    tags: {
      scenario: "upload-burst",
      service: "user-service",
      endpoint: "/api/v1/uploads",
    },
  });

  const ok = check(response, {
    "upload returns 201": (r) => r.status === 201,
    "upload queued": (r) => {
      try {
        return r.json("message") === "analysis queued";
      } catch (_) {
        return false;
      }
    },
  });

  if (ok) {
    uploadQueued.add(1);
  } else {
    uploadFailed.add(1);
    uploadFailureRate.add(1);
  }

  if (ok) {
    uploadFailureRate.add(0);
  }

  sleep(thinkTimeMs / 1000);
}

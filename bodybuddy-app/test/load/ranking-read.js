import http from "k6/http";
import { check, sleep } from "k6";
import { Counter, Rate } from "k6/metrics";

const scoreBaseUrl = __ENV.K6_SCORE_BASE_URL || "http://localhost:8081";
const rankingLimit = Number(__ENV.K6_RANKING_LIMIT || "10");
const thinkTimeMs = Number(__ENV.K6_THINK_TIME_MS || "100");

export const rankingSuccess = new Counter("bodybuddy_ranking_success_total");
export const rankingFailure = new Counter("bodybuddy_ranking_failure_total");
export const rankingFailureRate = new Rate("bodybuddy_ranking_failure_rate");

export const options = {
  stages: [
    { duration: "30s", target: 20 },
    { duration: "1m", target: 80 },
    { duration: "1m", target: 150 },
    { duration: "30s", target: 0 },
  ],
  thresholds: {
    http_req_failed: ["rate<0.03"],
    http_req_duration: ["p(95)<800"],
    bodybuddy_ranking_failure_rate: ["rate<0.03"],
  },
};

export default function () {
  const response = http.get(
    `${scoreBaseUrl}/api/v1/ranking?limit=${rankingLimit}`,
    {
      tags: {
        scenario: "ranking-read",
        service: "score-service",
        endpoint: "/api/v1/ranking",
      },
    },
  );

  const ok = check(response, {
    "ranking returns 200": (r) => r.status === 200,
    "ranking contains array": (r) => {
      try {
        return Array.isArray(r.json("ranking"));
      } catch (_) {
        return false;
      }
    },
  });

  if (ok) {
    rankingSuccess.add(1);
    rankingFailureRate.add(0);
  } else {
    rankingFailure.add(1);
    rankingFailureRate.add(1);
  }

  sleep(thinkTimeMs / 1000);
}

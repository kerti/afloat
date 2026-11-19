import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import type { components } from "@/api/schema.gen";

type Me = components["schemas"]["Me"];
type ErrorBody = components["schemas"]["Error"];

// Shapes are the generated contract types, so a contract change that breaks
// these fixtures fails the typecheck rather than drifting silently.
export const alice: Me = {
  id: "0192f0a0-0000-7000-8000-0000000000a1",
  household_id: "0192f0a0-0000-7000-8000-000000000001",
  email: "alice@example.com",
  display_name: "Alice",
  locale: "en-GB",
  time_zone: "Asia/Jakarta",
  household_display_name: "Test Household",
  reporting_currency: "IDR",
  period_start_day: 1,
  day_starts_at: "04:00",
  expected_monthly_income: null,
  allowance_mode: "adaptive",
};

export const envelope = (code: ErrorBody["code"], args?: ErrorBody["args"]): ErrorBody =>
  args ? { code, args } : { code };

// Default: signed out, local login enabled, alice/correct-horse signs in.
export function defaultHandlers(state = { signedIn: false, me: alice }) {
  return [
    http.get("/api/auth/methods", () => HttpResponse.json({ local: true, google: false })),
    http.get("/api/me", () =>
      state.signedIn
        ? HttpResponse.json(state.me)
        : HttpResponse.json(envelope("UNAUTHORIZED"), { status: 401 }),
    ),
    http.post("/api/auth/local/login", async ({ request }) => {
      const body = (await request.json()) as { email: string; password: string };
      if (body.email === state.me.email && body.password === "correct-horse") {
        state.signedIn = true;
        return new HttpResponse(null, { status: 204 });
      }
      return HttpResponse.json(envelope("INVALID_CREDENTIALS"), { status: 401 });
    }),
    http.post("/api/auth/logout", () => {
      state.signedIn = false;
      return new HttpResponse(null, { status: 204 });
    }),
  ];
}

export const server = setupServer();

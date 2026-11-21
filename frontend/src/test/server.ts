import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import type { components } from "@/api/schema.gen";

type Me = components["schemas"]["Me"];
type ErrorBody = components["schemas"]["Error"];
type RegisterBody = components["schemas"]["LocalRegisterRequest"];
type UpdateHouseholdBody = components["schemas"]["UpdateHouseholdRequest"];
type UpdateMeBody = components["schemas"]["UpdateMeRequest"];
type ChangeEmailBody = components["schemas"]["ChangeEmailRequest"];
type ChangePasswordBody = components["schemas"]["ChangePasswordRequest"];
type ChoiceBody = components["schemas"]["OnboardingChoiceRequest"];

// The handshake a registration opens, as the fake server holds it.
export type Handshake = { email: string; user_display_name: string; locale: string };

export type State = {
  signedIn: boolean;
  me: Me;
  handshake?: Handshake | null;
  // Defaults to open. Set false to run an instance with FOUNDING_DISABLED.
  foundingOpen?: boolean;
  // What the fake server was sent, for a test to assert on.
  registered?: RegisterBody;
  committed?: ChoiceBody;
  // Every PATCH /household body, in order.
  patches?: UpdateHouseholdBody[];
  // Every PATCH /me body, change-email body and change-password body, in order.
  mePatches?: UpdateMeBody[];
  emailChanges?: ChangeEmailBody[];
  passwordChanges?: ChangePasswordBody[];
  // The one password the fake server accepts as "current". Defaults to the
  // login fixture's.
  password?: string;
};

// Shapes are the generated contract types, so a contract change that breaks
// these fixtures fails the typecheck rather than drifting silently.
export const alice: Me = {
  id: "0192f0a0-0000-7000-8000-0000000000a1",
  email: "alice@example.com",
  user_display_name: "Alice",
  locale: "en-GB",
  time_zone: "Asia/Jakarta",
  household: {
    id: "0192f0a0-0000-7000-8000-000000000001",
    household_display_name: "Test Household",
    reporting_currency: "IDR",
    period_start_day: 1,
    day_starts_at: "04:00",
    allowance_mode: "adaptive",
  },
};

export const envelope = (code: ErrorBody["code"], args?: ErrorBody["args"]): ErrorBody =>
  args ? { code, args } : { code };

// Default: signed out, local login enabled, alice/correct-horse signs in, and
// anyone else may register and found a Household (alice's address is taken).
export function defaultHandlers(state: State = { signedIn: false, me: alice }) {
  return [
    http.get("/api/auth/methods", () =>
      HttpResponse.json({
        local: true,
        google: false,
        founding_open: state.foundingOpen ?? true,
      }),
    ),
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
    http.post("/api/auth/local/register", async ({ request }) => {
      const body = (await request.json()) as RegisterBody;
      if (body.email === alice.email) {
        return HttpResponse.json(envelope("EMAIL_TAKEN"), { status: 409 });
      }
      if ([...body.password].length < 10) {
        return HttpResponse.json(envelope("WEAK_PASSWORD", { reason: "min" }), { status: 400 });
      }
      if (body.password === "password1234") {
        return HttpResponse.json(envelope("WEAK_PASSWORD", { reason: "breached" }), {
          status: 400,
        });
      }
      state.registered = body;
      state.handshake = {
        email: body.email,
        user_display_name: body.user_display_name ?? body.email.split("@")[0]!,
        locale: body.locale ?? "en-GB",
      };
      return new HttpResponse(null, { status: 204 });
    }),
    http.get("/api/onboarding/options", () =>
      state.handshake
        ? HttpResponse.json({
            email: state.handshake.email,
            user_display_name: state.handshake.user_display_name,
            founding_open: state.foundingOpen ?? true,
          })
        : HttpResponse.json(envelope("UNAUTHORIZED"), { status: 401 }),
    ),
    http.post("/api/onboarding/choice", async ({ request }) => {
      const body = (await request.json()) as ChoiceBody;
      const hs = state.handshake;
      if (!hs) return HttpResponse.json(envelope("UNAUTHORIZED"), { status: 401 });
      if (state.foundingOpen === false) {
        return HttpResponse.json(envelope("FOUNDING_DISABLED"), { status: 403 });
      }
      state.committed = body;
      state.handshake = null;
      state.signedIn = true;
      state.me = {
        ...alice,
        email: hs.email,
        user_display_name: hs.user_display_name,
        locale: hs.locale as Me["locale"],
        household: {
          ...alice.household,
          household_display_name: body.household_display_name.trim(),
        },
      };
      return new HttpResponse(null, { status: 204 });
    }),
    http.patch("/api/household", async ({ request }) => {
      if (!state.signedIn) return HttpResponse.json(envelope("UNAUTHORIZED"), { status: 401 });
      const body = (await request.json()) as UpdateHouseholdBody;
      (state.patches ??= []).push(body);
      state.me = { ...state.me, household: { ...state.me.household, ...body } };
      return HttpResponse.json(state.me.household);
    }),
    http.patch("/api/me", async ({ request }) => {
      if (!state.signedIn) return HttpResponse.json(envelope("UNAUTHORIZED"), { status: 401 });
      const body = (await request.json()) as UpdateMeBody;
      (state.mePatches ??= []).push(body);
      state.me = { ...state.me, ...body };
      return HttpResponse.json(state.me);
    }),
    http.post("/api/auth/local/change-email", async ({ request }) => {
      if (!state.signedIn) return HttpResponse.json(envelope("UNAUTHORIZED"), { status: 401 });
      const body = (await request.json()) as ChangeEmailBody;
      (state.emailChanges ??= []).push(body);
      if (body.current_password !== (state.password ?? "correct-horse")) {
        return HttpResponse.json(envelope("INVALID_CURRENT_PASSWORD"), { status: 403 });
      }
      state.me = { ...state.me, email: body.email };
      return HttpResponse.json(state.me);
    }),
    http.post("/api/auth/local/change-password", async ({ request }) => {
      if (!state.signedIn) return HttpResponse.json(envelope("UNAUTHORIZED"), { status: 401 });
      const body = (await request.json()) as ChangePasswordBody;
      (state.passwordChanges ??= []).push(body);
      if (body.current_password !== (state.password ?? "correct-horse")) {
        return HttpResponse.json(envelope("INVALID_CURRENT_PASSWORD"), { status: 403 });
      }
      state.password = body.new_password;
      return new HttpResponse(null, { status: 204 });
    }),
    http.post("/api/auth/logout", () => {
      state.signedIn = false;
      return new HttpResponse(null, { status: 204 });
    }),
  ];
}

export const server = setupServer();

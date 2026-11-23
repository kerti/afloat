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
type Pocket = components["schemas"]["Pocket"];
type CreatePocketBody = components["schemas"]["CreatePocketRequest"];
type UpdatePocketBody = components["schemas"]["UpdatePocketRequest"];
type Category = components["schemas"]["Category"];
type CreateCategoryBody = components["schemas"]["CreateCategoryRequest"];
type Budget = components["schemas"]["Budget"];
type UpdateBudgetBody = components["schemas"]["UpdateBudgetRequest"];
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
  // The fake server's Pockets in creation order, and every POST /pockets body
  // it was sent, for a test to assert on.
  pockets?: Pocket[];
  pocketPosts?: CreatePocketBody[];
  // Every PATCH /pockets/{id} body, in order.
  pocketPatches?: UpdatePocketBody[];
  // Pockets the fake server has soft-deleted, and the ids of every DELETE and restore, in order.
  deletedPockets?: Pocket[];
  pocketDeletes?: string[];
  pocketRestores?: string[];
  // The fake server's Categories in creation order, and every POST /categories
  // body it was sent.
  categories?: Category[];
  categoryPosts?: CreateCategoryBody[];
  // The fake server's Budget for the current Period (absent: a first read
  // answers an empty one), and every PATCH /budget body, in order.
  budget?: Budget;
  budgetPatches?: UpdateBudgetBody[];
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
  appearance: "system",
  household: {
    id: "0192f0a0-0000-7000-8000-000000000001",
    household_display_name: "Test Household",
    reporting_currency: "IDR",
    period_start_day: 1,
    day_starts_at: "04:00",
    allowance_mode: "adaptive",
  },
};

// A Household's first read of a Period: a window and nothing planned yet.
export const emptyBudget: Budget = {
  period: { year_month: "2026-11", starts_on: "2026-11-01", ends_on: "2026-11-30" },
  allocations: [],
  commitments_budget: "0.0000",
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
    http.get("/api/pockets", () => {
      if (!state.signedIn) return HttpResponse.json(envelope("UNAUTHORIZED"), { status: 401 });
      return HttpResponse.json({ items: state.pockets ?? [] });
    }),
    http.post("/api/pockets", async ({ request }) => {
      if (!state.signedIn) return HttpResponse.json(envelope("UNAUTHORIZED"), { status: 401 });
      const body = (await request.json()) as CreatePocketBody;
      (state.pocketPosts ??= []).push(body);
      const list = (state.pockets ??= []);
      // As the server: the body is validated before the id is considered.
      const name = body.pocket_display_name.trim();
      if (name === "") {
        return HttpResponse.json(
          envelope("VALIDATION", { field: "pocket_display_name", rule: "required" }),
          { status: 400 },
        );
      }
      const held = list.find((p) => p.id === body.id);
      if (held) return HttpResponse.json(held, { status: 200 });
      if (list.some((p) => p.pocket_display_name.toLowerCase() === name.toLowerCase())) {
        return HttpResponse.json(envelope("NAME_TAKEN"), { status: 409 });
      }
      const pocket: Pocket = { id: body.id, pocket_display_name: name, active: true };
      list.push(pocket);
      return HttpResponse.json(pocket, { status: 201 });
    }),
    http.patch("/api/pockets/:id", async ({ request, params }) => {
      if (!state.signedIn) return HttpResponse.json(envelope("UNAUTHORIZED"), { status: 401 });
      const body = (await request.json()) as UpdatePocketBody;
      (state.pocketPatches ??= []).push(body);
      const list = (state.pockets ??= []);
      const held = list.find((p) => p.id === params.id);
      if (!held) return HttpResponse.json(envelope("NOT_FOUND"), { status: 404 });
      if (body.pocket_display_name !== undefined) {
        const name = body.pocket_display_name.trim();
        if (name === "") {
          return HttpResponse.json(
            envelope("VALIDATION", { field: "pocket_display_name", rule: "required" }),
            { status: 400 },
          );
        }
        if (
          list.some(
            (p) => p.id !== held.id && p.pocket_display_name.toLowerCase() === name.toLowerCase(),
          )
        ) {
          return HttpResponse.json(envelope("NAME_TAKEN"), { status: 409 });
        }
        held.pocket_display_name = name;
      }
      if (body.active !== undefined) held.active = body.active;
      return HttpResponse.json(held);
    }),
    // Soft delete: the Pocket leaves the list but is kept, so restore can bring it back.
    http.delete("/api/pockets/:id", ({ params }) => {
      if (!state.signedIn) return HttpResponse.json(envelope("UNAUTHORIZED"), { status: 401 });
      const list = (state.pockets ??= []);
      const held = list.find((p) => p.id === params.id);
      if (!held) return HttpResponse.json(envelope("NOT_FOUND"), { status: 404 });
      state.pocketDeletes = [...(state.pocketDeletes ?? []), held.id];
      (state.deletedPockets ??= []).push(held);
      state.pockets = list.filter((p) => p !== held);
      return new HttpResponse(null, { status: 204 });
    }),
    http.post("/api/pockets/:id/restore", ({ params }) => {
      if (!state.signedIn) return HttpResponse.json(envelope("UNAUTHORIZED"), { status: 401 });
      const gone = state.deletedPockets?.find((p) => p.id === params.id);
      if (!gone) return HttpResponse.json(envelope("NOT_FOUND"), { status: 404 });
      state.pocketRestores = [...(state.pocketRestores ?? []), gone.id];
      const list = (state.pockets ??= []);
      const taken = list.some(
        (p) => p.pocket_display_name.toLowerCase() === gone.pocket_display_name.toLowerCase(),
      );
      if (taken) return HttpResponse.json(envelope("NAME_TAKEN"), { status: 409 });
      state.deletedPockets = state.deletedPockets!.filter((p) => p !== gone);
      list.push(gone);
      return HttpResponse.json(gone);
    }),
    http.get("/api/categories", () => {
      if (!state.signedIn) return HttpResponse.json(envelope("UNAUTHORIZED"), { status: 401 });
      return HttpResponse.json({ items: state.categories ?? [] });
    }),
    http.post("/api/categories", async ({ request }) => {
      if (!state.signedIn) return HttpResponse.json(envelope("UNAUTHORIZED"), { status: 401 });
      const body = (await request.json()) as CreateCategoryBody;
      (state.categoryPosts ??= []).push(body);
      const list = (state.categories ??= []);
      const name = body.category_display_name.trim();
      if (name === "") {
        return HttpResponse.json(
          envelope("VALIDATION", { field: "category_display_name", rule: "required" }),
          { status: 400 },
        );
      }
      const held = list.find((c) => c.id === body.id);
      if (held) return HttpResponse.json(held, { status: 200 });
      if (list.some((c) => c.category_display_name.toLowerCase() === name.toLowerCase())) {
        return HttpResponse.json(envelope("NAME_TAKEN"), { status: 409 });
      }
      if (body.bucket === "commitment" && (body.allocation ?? null) === null) {
        return HttpResponse.json(
          envelope("VALIDATION", { field: "allocation", rule: "required" }),
          {
            status: 400,
          },
        );
      }
      const category: Category = {
        id: body.id,
        category_display_name: name,
        bucket: body.bucket,
        active: true,
      };
      list.push(category);
      if (body.allocation != null) {
        // As the server: the allocation joins the Budget and the figures follow.
        const held = (state.budget ??= emptyBudget);
        const [int = "0", frac = ""] = body.allocation.split(".");
        const amount = `${int}.${frac.padEnd(4, "0")}`;
        const allocations = [...held.allocations, { category_id: body.id, allocation: amount }];
        const sum = (bucket: Category["bucket"]) =>
          allocations
            .filter((a) => list.find((c) => c.id === a.category_id)?.bucket === bucket)
            .reduce((n, a) => n + Number(a.allocation), 0);
        const commitments = sum("commitment");
        const fixed = (n: number) => n.toFixed(4);
        state.budget = {
          ...held,
          allocations,
          commitments_budget: fixed(commitments),
          ...(held.monthly_budget !== undefined && {
            daily_pool: fixed(Number(held.monthly_budget) - commitments),
            unallocated: fixed(Number(held.monthly_budget) - commitments - sum("daily")),
          }),
        };
      }
      return HttpResponse.json(category, { status: 201 });
    }),
    http.get("/api/budget", () => {
      if (!state.signedIn) return HttpResponse.json(envelope("UNAUTHORIZED"), { status: 401 });
      return HttpResponse.json((state.budget ??= emptyBudget));
    }),
    http.patch("/api/budget", async ({ request }) => {
      if (!state.signedIn) return HttpResponse.json(envelope("UNAUTHORIZED"), { status: 401 });
      const body = (await request.json()) as UpdateBudgetBody;
      (state.budgetPatches ??= []).push(body);
      const held = (state.budget ??= emptyBudget);
      if (body.monthly_budget !== undefined) {
        // As the server: the amount comes back at exactly four decimals.
        const [int = "0", frac = ""] = body.monthly_budget.split(".");
        const amount = `${int}.${frac.padEnd(4, "0")}`;
        state.budget = { ...held, monthly_budget: amount, daily_pool: amount, unallocated: amount };
      }
      return HttpResponse.json(state.budget);
    }),
    http.post("/api/auth/logout", () => {
      state.signedIn = false;
      return new HttpResponse(null, { status: 204 });
    }),
  ];
}

export const server = setupServer();

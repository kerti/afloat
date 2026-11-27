import type { components } from "./schema.gen";

export type ErrorCode = components["schemas"]["ErrorCode"];
export type Me = components["schemas"]["Me"];
export type Household = components["schemas"]["Household"];
export type UpdateHouseholdRequest = components["schemas"]["UpdateHouseholdRequest"];
export type UpdateMeRequest = components["schemas"]["UpdateMeRequest"];
export type ChangePasswordRequest = components["schemas"]["ChangePasswordRequest"];
export type ChangeEmailRequest = components["schemas"]["ChangeEmailRequest"];
export type Pocket = components["schemas"]["Pocket"];
export type PocketList = components["schemas"]["PocketList"];
export type CreatePocketRequest = components["schemas"]["CreatePocketRequest"];
export type UpdatePocketRequest = components["schemas"]["UpdatePocketRequest"];
export type Category = components["schemas"]["Category"];
export type CategoryList = components["schemas"]["CategoryList"];
export type CreateCategoryRequest = components["schemas"]["CreateCategoryRequest"];
export type UpdateCategoryRequest = components["schemas"]["UpdateCategoryRequest"];
export type Expense = components["schemas"]["Expense"];
export type ExpenseList = components["schemas"]["ExpenseList"];
export type CreateExpenseRequest = components["schemas"]["CreateExpenseRequest"];
export type UpdateExpenseRequest = components["schemas"]["UpdateExpenseRequest"];
export type Bucket = components["schemas"]["Bucket"];
export type Budget = components["schemas"]["Budget"];
export type UpdateBudgetRequest = components["schemas"]["UpdateBudgetRequest"];
export type Dashboard = components["schemas"]["Dashboard"];
export type CategoryFigure = components["schemas"]["CategoryFigure"];
export type PocketFigure = components["schemas"]["PocketFigure"];
export type Commitments = components["schemas"]["Commitments"];
export type CommitmentFigure = components["schemas"]["CommitmentFigure"];
export type Trajectory = components["schemas"]["Trajectory"];
export type Chart = components["schemas"]["Chart"];
export type ChartPoint = components["schemas"]["ChartPoint"];
export type AuthMethods = components["schemas"]["AuthMethods"];
export type LocalLoginRequest = components["schemas"]["LocalLoginRequest"];
export type LocalRegisterRequest = components["schemas"]["LocalRegisterRequest"];
export type OnboardingOptions = components["schemas"]["OnboardingOptions"];
export type OnboardingChoiceRequest = components["schemas"]["OnboardingChoiceRequest"];

// The contract's base path (servers[0].url). Same origin as the page: the Go
// binary serves both in production, the Vite proxy in development.
const base = "/api";

// What a failed call means to the UI. `code` is the contract's ErrorCode, or
// one of two the client mints itself: NETWORK when no answer arrived (PRD
// N14: a write must fail visibly, never silently) and UNKNOWN when one arrived
// without the envelope. The frontend owns every string (CLAUDE.md rule 7).
export class ApiError extends Error {
  readonly status: number;
  readonly code: ErrorCode | "NETWORK" | "UNKNOWN";
  readonly args: Record<string, unknown>;
  readonly retryAfterSeconds?: number;

  constructor(
    status: number,
    code: ApiError["code"],
    args: Record<string, unknown> = {},
    retryAfterSeconds?: number,
  ) {
    super(code);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.args = args;
    this.retryAfterSeconds = retryAfterSeconds;
  }
}

// A Record over the generated union, so adding a code to the contract and
// regenerating fails the typecheck here until the client knows it too.
const errorCodeKnown: Record<ErrorCode, true> = {
  VALIDATION: true,
  INVALID_JSON_BODY: true,
  INVALID_CREDENTIALS: true,
  TOO_MANY_ATTEMPTS: true,
  UNAUTHORIZED: true,
  CROSS_SITE_REQUEST_BLOCKED: true,
  EMAIL_TAKEN: true,
  WEAK_PASSWORD: true,
  FOUNDING_DISABLED: true,
  INVALID_CURRENT_PASSWORD: true,
  ID_TAKEN: true,
  NAME_TAKEN: true,
  CATEGORY_IN_USE: true,
  POCKET_IN_USE: true,
  CURRENCY_LOCKED: true,
  LAST_POCKET: true,
  NOT_FOUND: true,
  INTERNAL: true,
};
export const errorCodes = Object.keys(errorCodeKnown) as ErrorCode[];
const knownCodes: ReadonlySet<string> = new Set(errorCodes);

async function toApiError(res: Response): Promise<ApiError> {
  const retryAfter = Number.parseInt(res.headers.get("Retry-After") ?? "", 10);
  const retryAfterSeconds = Number.isFinite(retryAfter) && retryAfter >= 0 ? retryAfter : undefined;
  try {
    const body = (await res.json()) as Partial<components["schemas"]["Error"]>;
    if (typeof body.code === "string" && knownCodes.has(body.code)) {
      return new ApiError(res.status, body.code, body.args ?? {}, retryAfterSeconds);
    }
  } catch {
    // Not JSON: an unregistered-path 404, a proxy's error page.
  }
  return new ApiError(res.status, "UNKNOWN", {}, retryAfterSeconds);
}

async function request(path: string, init: RequestInit = {}): Promise<Response> {
  let res: Response;
  try {
    res = await fetch(base + path, { credentials: "same-origin", ...init });
  } catch {
    throw new ApiError(0, "NETWORK");
  }
  if (!res.ok) throw await toApiError(res);
  return res;
}

export async function getMe(): Promise<Me> {
  return (await request("/me")).json() as Promise<Me>;
}

export async function getAuthMethods(): Promise<AuthMethods> {
  return (await request("/auth/methods")).json() as Promise<AuthMethods>;
}

export async function localLogin(body: LocalLoginRequest): Promise<void> {
  await request("/auth/local/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

// Registration opens a handshake and answers 204; the cookie it sets is the
// onboarding gate's credential, held by the browser and never read here.
export async function localRegister(body: LocalRegisterRequest): Promise<void> {
  await request("/auth/local/register", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

export async function getOnboardingOptions(): Promise<OnboardingOptions> {
  return (await request("/onboarding/options")).json() as Promise<OnboardingOptions>;
}

export async function commitOnboardingChoice(body: OnboardingChoiceRequest): Promise<void> {
  await request("/onboarding/choice", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

export async function updateHousehold(body: UpdateHouseholdRequest): Promise<Household> {
  return (
    await request("/household", {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    })
  ).json() as Promise<Household>;
}

export async function updateMe(body: UpdateMeRequest): Promise<Me> {
  return (
    await request("/me", {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    })
  ).json() as Promise<Me>;
}

export async function changeEmail(body: ChangeEmailRequest): Promise<Me> {
  return (
    await request("/auth/local/change-email", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    })
  ).json() as Promise<Me>;
}

// Answers 204 and sets a fresh session cookie the browser keeps; the client
// reads none and holds no token.
export async function changePassword(body: ChangePasswordRequest): Promise<void> {
  await request("/auth/local/change-password", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

export async function logout(): Promise<void> {
  await request("/auth/logout", { method: "POST" });
}

export async function listPockets(): Promise<PocketList> {
  return (await request("/pockets")).json() as Promise<PocketList>;
}

// Idempotent on the client's id: 201 the first time, 200 with the Pocket as it
// stands on a replay. Both are a success and both resolve to the Pocket.
export async function createPocket(body: CreatePocketRequest): Promise<Pocket> {
  return (
    await request("/pockets", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    })
  ).json() as Promise<Pocket>;
}

// Merge update: only the fields sent change. The answer is the Pocket as it
// now stands. 404 NOT_FOUND covers unknown, another Household's and deleted.
export async function updatePocket(id: string, body: UpdatePocketRequest): Promise<Pocket> {
  return (
    await request(`/pockets/${encodeURIComponent(id)}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    })
  ).json() as Promise<Pocket>;
}

// A soft delete, answered with an empty 204. Deleting a Pocket that is already
// deleted is a 204 too; 404 NOT_FOUND covers unknown and another Household's.
export async function deletePocket(id: string): Promise<void> {
  await request(`/pockets/${encodeURIComponent(id)}`, { method: "DELETE" });
}

// The Undo of a delete: 200 with the Pocket, 409 NAME_TAKEN when another Pocket
// has taken its name meanwhile, 404 NOT_FOUND when it is not deleted.
export async function restorePocket(id: string): Promise<Pocket> {
  return (
    await request(`/pockets/${encodeURIComponent(id)}/restore`, { method: "POST" })
  ).json() as Promise<Pocket>;
}

// The current Budget Period's Budget. The first read of a Period may write it
// (the server carries the last Period forward); the figures are computed there,
// and the client derives none.
export async function getBudget(): Promise<Budget> {
  return (await request("/budget")).json() as Promise<Budget>;
}

// Merge update: only the fields sent change. The answer is the whole Budget.
export async function updateBudget(body: UpdateBudgetRequest): Promise<Budget> {
  return (
    await request("/budget", {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    })
  ).json() as Promise<Budget>;
}

// The Household's figures for the Budget Period it is in, as one answer. Every
// figure is computed there, and the client derives none; `trajectory` is absent
// while there is no Monthly Budget. Like the Budget's, the first read of a
// Period may write it (the server carries the last Period forward).
export async function getDashboard(): Promise<Dashboard> {
  return (await request("/dashboard")).json() as Promise<Dashboard>;
}

export async function listCategories(): Promise<CategoryList> {
  return (await request("/categories")).json() as Promise<CategoryList>;
}

// Idempotent on the client's id: 201 the first time, 200 with the Category as it
// stands on a replay (its allocation is then neither compared nor applied).
export async function createCategory(body: CreateCategoryRequest): Promise<Category> {
  return (
    await request("/categories", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    })
  ).json() as Promise<Category>;
}

// Merge update: only the fields sent change (`bucket` and the allocation are not
// accepted here; the allocation is the Budget's). 404 NOT_FOUND, 409 NAME_TAKEN.
export async function updateCategory(id: string, body: UpdateCategoryRequest): Promise<Category> {
  return (
    await request(`/categories/${encodeURIComponent(id)}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    })
  ).json() as Promise<Category>;
}

// A soft delete, answered with an empty 204; a repeat is a 204 too. 404 NOT_FOUND
// covers unknown and another Household's; 409 CATEGORY_IN_USE when another
// Budget Period holds an allocation for it.
export async function deleteCategory(id: string): Promise<void> {
  await request(`/categories/${encodeURIComponent(id)}`, { method: "DELETE" });
}

// The Undo of a delete: 200 with the Category (a Commitment comes back with its
// allocation), 409 NAME_TAKEN when another Category has taken its name meanwhile.
export async function restoreCategory(id: string): Promise<Category> {
  return (
    await request(`/categories/${encodeURIComponent(id)}/restore`, { method: "POST" })
  ).json() as Promise<Category>;
}

// One page of the Household's Expenses, newest first. `after` is the previous
// page's opaque `next`; a page without `next` is the last.
export async function listExpenses(
  after?: string,
  uncategorised?: boolean,
  deleted?: boolean,
): Promise<ExpenseList> {
  const params = new URLSearchParams();
  if (after !== undefined) params.set("after", after);
  if (uncategorised) params.set("uncategorised", "true");
  // The Recycle Bin: only deleted Expenses, the last deleted first.
  if (deleted) params.set("deleted", "true");
  const query = params.size > 0 ? `?${params.toString()}` : "";
  return (await request(`/expenses${query}`)).json() as Promise<ExpenseList>;
}

// One live Expense. 404 NOT_FOUND covers unknown, another Household's and deleted.
export async function getExpense(id: string): Promise<Expense> {
  return (await request(`/expenses/${encodeURIComponent(id)}`)).json() as Promise<Expense>;
}

// A soft delete: 204, and the Expense is in the Recycle Bin. Repeat is 204 too.
export async function deleteExpense(id: string): Promise<void> {
  await request(`/expenses/${encodeURIComponent(id)}`, { method: "DELETE" });
}

// Takes an Expense out of the Recycle Bin: 200 with the Expense as it was. 404
// NOT_FOUND when it is unknown or another Household's.
export async function restoreExpense(id: string): Promise<Expense> {
  return (
    await request(`/expenses/${encodeURIComponent(id)}/restore`, { method: "POST" })
  ).json() as Promise<Expense>;
}

// Idempotent on the client's id: 201 the first time, 200 with the Expense as it
// stands on a replay. Both are a success and both resolve to the Expense.
export async function createExpense(body: CreateExpenseRequest): Promise<Expense> {
  return (
    await request("/expenses", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    })
  ).json() as Promise<Expense>;
}

// Merge update: only the fields sent change; `null` clears the time, Category or
// description. The answer is the Expense as it now stands. 404 NOT_FOUND covers
// unknown, another Household's and deleted.
export async function updateExpense(id: string, body: UpdateExpenseRequest): Promise<Expense> {
  return (
    await request(`/expenses/${encodeURIComponent(id)}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    })
  ).json() as Promise<Expense>;
}

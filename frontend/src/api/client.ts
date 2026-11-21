import type { components } from "./schema.gen";

export type ErrorCode = components["schemas"]["ErrorCode"];
export type Me = components["schemas"]["Me"];
export type Household = components["schemas"]["Household"];
export type UpdateHouseholdRequest = components["schemas"]["UpdateHouseholdRequest"];
export type UpdateMeRequest = components["schemas"]["UpdateMeRequest"];
export type ChangePasswordRequest = components["schemas"]["ChangePasswordRequest"];
export type ChangeEmailRequest = components["schemas"]["ChangeEmailRequest"];
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

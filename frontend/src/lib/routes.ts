// Every path the app links or navigates to, in one place, so a renamed path is
// one edit and a mistyped link is a type error (docs/agents/ui-conventions.md;
// Balances frontend/src/lib/routes.ts, github.com/kerti/balances-v2). A
// component never writes a path literal; an ESLint rule enforces it.
export const routes = {
  home: "/",
  login: "/login",
  register: "/register",
  onboarding: "/onboarding",
  settings: "/settings",
  budget: "/budget",
  expenses: "/expenses",
  capture: "/capture",
  uncategorisedExpenses: "/expenses?uncategorised=true",
  recycleBin: "/expenses/recycle-bin",
  // The edit route's pattern, for <Route path>; `expense(id)` builds a link.
  expensePattern: "/expenses/:id",
  expense: (id: string) => `/expenses/${encodeURIComponent(id)}`,
} as const;

// Paths that used to be screens. Each only redirects to /settings, keeping the
// query string, so an old link (or an old ?edit=pocket:<id>) still lands.
export const legacyRedirects = {
  householdSettings: "/settings/household",
  userSettings: "/settings/user",
  pockets: "/pockets",
} as const;

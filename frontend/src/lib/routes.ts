// Every path the app links or navigates to, in one place, so a renamed path is
// one edit and a mistyped link is a type error (docs/agents/ui-conventions.md;
// Balances frontend/src/lib/routes.ts, github.com/kerti/balances-v2). A
// component never writes a path literal; an ESLint rule enforces it.
export const routes = {
  home: "/",
  login: "/login",
  register: "/register",
  onboarding: "/onboarding",
  householdSettings: "/settings/household",
  userSettings: "/settings/user",
  pockets: "/pockets",
} as const;

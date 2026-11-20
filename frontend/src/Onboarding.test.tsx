import { describe, expect, it } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { alice, defaultHandlers, envelope, server, type State } from "@/test/server";
import { renderApp } from "@/test/render";
import { i18n } from "@/i18n";

const fresh = (extra: Partial<State> = {}): State => ({ signedIn: false, me: alice, ...extra });

async function register(
  user: ReturnType<typeof userEvent.setup>,
  { email = "dewi@example.com", password = "kucing oranye di atap", name = "" } = {},
) {
  await user.type(await screen.findByLabelText("Email"), email);
  await user.type(screen.getByLabelText("Password"), password);
  if (name) await user.type(screen.getByLabelText("Your name (optional)"), name);
  await user.click(screen.getByRole("button", { name: "Continue" }));
}

describe("create account", () => {
  it("is offered on the sign-in screen when local sign-in and founding are open", async () => {
    server.use(...defaultHandlers());
    renderApp("/login");
    expect(await screen.findByRole("link", { name: "Create account" })).toHaveAttribute(
      "href",
      "/register",
    );
  });

  it("is not offered when the operator has closed founding", async () => {
    server.use(...defaultHandlers(fresh({ foundingOpen: false })));
    renderApp("/login");
    await screen.findByLabelText("Password");
    // The methods answer has settled by the time the form shows, but give the
    // query one tick more to be sure the absence is not just "not yet".
    await new Promise((r) => setTimeout(r, 20));
    expect(screen.queryByRole("link", { name: "Create account" })).not.toBeInTheDocument();
  });

  it("is not offered when local sign-in is off", async () => {
    server.use(
      http.get("/api/auth/methods", () =>
        HttpResponse.json({ local: false, google: true, founding_open: true }),
      ),
      ...defaultHandlers(),
    );
    renderApp("/login");
    await screen.findByText(/Password sign-in is turned off/);
    expect(screen.queryByRole("link", { name: "Create account" })).not.toBeInTheDocument();
  });

  it("registers, names the Household at the gate, and lands on the shell", async () => {
    const state = fresh();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/login");

    await user.click(await screen.findByRole("link", { name: "Create account" }));
    await register(user, { name: "  Dewi Putri " });

    // The gate: the suggestion comes from the catalogue, in the person's own name.
    const field = await screen.findByLabelText("Household name");
    expect(field).toHaveValue("Dewi Putri's Household");
    expect(state.registered).toEqual({
      email: "dewi@example.com",
      password: "kucing oranye di atap",
      user_display_name: "Dewi Putri",
      locale: "en-GB",
    });

    await user.click(screen.getByRole("button", { name: "Create Household" }));

    // /me was invalidated, so the shell answers as the new User.
    expect(await screen.findByText("Signed in as Dewi Putri")).toBeInTheDocument();
    expect(screen.getByText("Household: Dewi Putri's Household")).toBeInTheDocument();
    expect(state.committed).toEqual({
      found: true,
      household_display_name: "Dewi Putri's Household",
    });
  });

  it("registers without a name and the gate falls back to what the server derived", async () => {
    const state = fresh();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/register");

    await register(user);

    expect(await screen.findByLabelText("Household name")).toHaveValue("dewi's Household");
    // No user_display_name key at all, so the server defaults it.
    expect(state.registered).not.toHaveProperty("user_display_name");
  });

  it("pre-fills the Household name in Indonesian from the catalogue", async () => {
    const state = fresh();
    server.use(...defaultHandlers(state));
    await i18n.changeLanguage("id-ID");
    const user = userEvent.setup();
    renderApp("/register");

    await user.type(await screen.findByLabelText("Email"), "dewi@example.com");
    await user.type(screen.getByLabelText("Kata sandi"), "kucing oranye di atap");
    await user.type(screen.getByLabelText("Nama Anda (opsional)"), "Dewi");
    await user.click(screen.getByRole("button", { name: "Lanjut" }));

    expect(await screen.findByLabelText("Nama rumah tangga")).toHaveValue("Rumah Tangga Dewi");
    // The locale sent is the language the form was read in.
    expect(state.registered?.locale).toBe("id-ID");
  });

  it("commits the name the person typed, not the suggestion", async () => {
    const state = fresh();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/register");
    await register(user, { name: "Dewi" });

    const field = await screen.findByLabelText("Household name");
    await user.clear(field);
    await user.type(field, "  Kos Kuning ");
    await user.click(screen.getByRole("button", { name: "Create Household" }));

    expect(await screen.findByText("Household: Kos Kuning")).toBeInTheDocument();
    // Sent as typed: trimming is the server's.
    expect(state.committed?.household_display_name).toBe("  Kos Kuning ");
  });

  it("says an address is taken in words, with the way to sign in", async () => {
    server.use(...defaultHandlers());
    const user = userEvent.setup();
    renderApp("/register");

    await register(user, { email: "alice@example.com" });

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "An account with that email already exists. Sign in instead.",
    );
    expect(screen.queryByText(/EMAIL_TAKEN/)).not.toBeInTheDocument();
  });

  it("words each weak-password reason, in both languages", async () => {
    server.use(...defaultHandlers());
    const user = userEvent.setup();
    renderApp("/register");

    await register(user, { password: "short" });
    expect(await screen.findByRole("alert")).toHaveTextContent("Use at least 10 characters.");

    await user.clear(screen.getByLabelText("Password"));
    await user.type(screen.getByLabelText("Password"), "password1234");
    await user.click(screen.getByRole("button", { name: "Continue" }));
    expect(await screen.findByText("That password is too common. Pick another.")).toBeVisible();

    await i18n.changeLanguage("id-ID");
    expect(await screen.findByText("Kata sandi itu terlalu umum. Pilih yang lain.")).toBeVisible();
    expect(screen.queryByText(/WEAK_PASSWORD/)).not.toBeInTheDocument();
  });

  it("reads the backoff window from Retry-After", async () => {
    server.use(
      http.post("/api/auth/local/register", () =>
        HttpResponse.json(envelope("TOO_MANY_ATTEMPTS"), {
          status: 429,
          headers: { "Retry-After": "7" },
        }),
      ),
      ...defaultHandlers(),
    );
    const user = userEvent.setup();
    renderApp("/register");

    await register(user);
    expect(await screen.findByRole("alert")).toHaveTextContent("Try again in 7 seconds.");
  });

  it("explains, instead of showing a form, when founding is closed", async () => {
    server.use(...defaultHandlers(fresh({ foundingOpen: false })));
    renderApp("/register");
    expect(await screen.findByText(/isn't accepting new accounts/)).toBeInTheDocument();
    expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
  });

  it("explains, instead of showing a form, when local sign-in is off", async () => {
    server.use(
      http.get("/api/auth/methods", () =>
        HttpResponse.json({ local: false, google: true, founding_open: true }),
      ),
      ...defaultHandlers(),
    );
    renderApp("/register");
    expect(await screen.findByText(/Creating accounts needs password sign-in/)).toBeInTheDocument();
    expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
  });
});

describe("the onboarding gate", () => {
  it("sends a visitor with no handshake to sign in", async () => {
    server.use(...defaultHandlers());
    renderApp("/onboarding");
    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
  });

  it("sends the person to sign in when the handshake lapses before they commit", async () => {
    // The gate loaded; the handshake expired (or was spent in another tab) since.
    server.use(
      http.post("/api/onboarding/choice", () =>
        HttpResponse.json(envelope("UNAUTHORIZED"), { status: 401 }),
      ),
      ...defaultHandlers(
        fresh({ handshake: { email: "d@example.com", user_display_name: "D", locale: "en-GB" } }),
      ),
    );
    const user = userEvent.setup();
    renderApp("/onboarding");

    await user.click(await screen.findByRole("button", { name: "Create Household" }));
    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
  });

  it("explains a closed instance when the commit answers FOUNDING_DISABLED", async () => {
    // The options said open; the operator closed it since (display only).
    server.use(
      http.post("/api/onboarding/choice", () =>
        HttpResponse.json(envelope("FOUNDING_DISABLED"), { status: 403 }),
      ),
      ...defaultHandlers(
        fresh({ handshake: { email: "d@example.com", user_display_name: "D", locale: "en-GB" } }),
      ),
    );
    const user = userEvent.setup();
    renderApp("/onboarding");

    await user.click(await screen.findByRole("button", { name: "Create Household" }));

    expect(await screen.findByText(/isn't accepting new Households/)).toBeInTheDocument();
    expect(screen.queryByText(/FOUNDING_DISABLED/)).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Household name")).not.toBeInTheDocument();
  });

  it("shows the explanation up front when the options say founding is closed", async () => {
    server.use(
      ...defaultHandlers(
        fresh({
          foundingOpen: false,
          handshake: { email: "d@example.com", user_display_name: "D", locale: "en-GB" },
        }),
      ),
    );
    renderApp("/onboarding");
    expect(await screen.findByText(/isn't accepting new Households/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Create Household" })).not.toBeInTheDocument();
  });

  it("says the address was taken at the commit, and offers sign-in", async () => {
    server.use(
      http.post("/api/onboarding/choice", () =>
        HttpResponse.json(envelope("EMAIL_TAKEN"), { status: 409 }),
      ),
      ...defaultHandlers(
        fresh({ handshake: { email: "d@example.com", user_display_name: "D", locale: "en-GB" } }),
      ),
    );
    const user = userEvent.setup();
    renderApp("/onboarding");

    await user.click(await screen.findByRole("button", { name: "Create Household" }));

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("An account with that email already exists.");
    expect(screen.getByRole("link", { name: "Sign in" })).toHaveAttribute("href", "/login");
  });

  it("shows a blank-name VALIDATION in words, and the form stays", async () => {
    server.use(
      http.post("/api/onboarding/choice", () =>
        HttpResponse.json(
          envelope("VALIDATION", { field: "household_display_name", rule: "required" }),
          {
            status: 400,
          },
        ),
      ),
      ...defaultHandlers(
        fresh({ handshake: { email: "d@example.com", user_display_name: "D", locale: "en-GB" } }),
      ),
    );
    const user = userEvent.setup();
    renderApp("/onboarding");

    await user.clear(await screen.findByLabelText("Household name"));
    await user.click(screen.getByRole("button", { name: "Create Household" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Check the household name field.");
    expect(screen.getByLabelText("Household name")).toBeInTheDocument();
  });

  it("leaves the gate for the shell when already signed in", async () => {
    server.use(...defaultHandlers({ signedIn: true, me: alice }));
    renderApp("/onboarding");
    expect(await screen.findByText("Signed in as Alice")).toBeInTheDocument();
  });
});

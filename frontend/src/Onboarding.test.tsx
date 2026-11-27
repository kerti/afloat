import { describe, expect, it } from "vitest";
import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { alice, defaultHandlers, envelope, server, type State } from "@/test/server";
import { renderApp } from "@/test/render";
import { i18n } from "@/i18n";
import enGB from "@/locales/en-GB.json";

const fresh = (extra: Partial<State> = {}): State => ({ signedIn: false, me: alice, ...extra });

// The Home notice, found by its dismiss button.
const noticeCard = () =>
  screen.getByRole("button", { name: /^(Dismiss|Tutup)$/ }).closest("section")!;

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

  it("registers, founds silently with both names, and lands on Home with the notice", async () => {
    const state = fresh();
    server.use(...defaultHandlers(state));
    // The handshake is spent by the first commit, so a second would answer 401.
    let commits = 0;
    const countCommits = ({ request }: { request: Request }) => {
      if (
        request.method === "POST" &&
        new URL(request.url).pathname.endsWith("/onboarding/choice")
      ) {
        commits++;
      }
    };
    server.events.on("request:start", countCommits);
    const user = userEvent.setup();
    renderApp("/login");

    await user.click(await screen.findByRole("link", { name: "Create account" }));
    await register(user, { name: "  Dewi Putri " });

    // No gate: the person is on Home, signed in as the new User.
    expect(await screen.findByRole("heading", { name: "Home" })).toBeInTheDocument();
    expect(screen.queryByLabelText("Household name")).not.toBeInTheDocument();
    expect(state.registered).toEqual({
      email: "dewi@example.com",
      password: "kucing oranye di atap",
      user_display_name: "Dewi Putri",
      locale: "en-GB",
    });
    expect(state.committed).toEqual({
      found: true,
      household_display_name: "Dewi Putri's Household",
      pocket_display_name: "Cash",
    });
    expect(noticeCard()).toHaveTextContent(
      "We named your Household Dewi Putri's Household and your first Pocket Cash, and set your currency to IDR.",
    );
    expect(noticeCard()).toHaveTextContent(
      "Rename either any time. Change the currency before your first Expense, because it can't change after that.",
    );
    expect(within(noticeCard()).getByRole("link", { name: "Go to Settings" })).toHaveAttribute(
      "href",
      "/settings",
    );
    expect(commits).toBe(1);
    server.events.removeListener("request:start", countCommits);
  });

  it("names the Household from what the server derived when no name was given", async () => {
    const state = fresh();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/register");

    await register(user);

    expect(await screen.findByRole("heading", { name: "Home" })).toBeInTheDocument();
    // Home's own read has answered, so nothing is still in flight to leak.
    expect(await screen.findByText(enGB.dashboard.noBudget)).toBeInTheDocument();
    expect(screen.getByText("dewi's Household", { selector: "strong" })).toBeInTheDocument();
    expect(state.committed?.household_display_name).toBe("dewi's Household");
    // No user_display_name key at all, so the server defaults it.
    expect(state.registered).not.toHaveProperty("user_display_name");
  });

  it("founds in Indonesian from the catalogue, and the notice reads in Indonesian", async () => {
    const state = fresh();
    server.use(...defaultHandlers(state));
    await i18n.changeLanguage("id-ID");
    const user = userEvent.setup();
    renderApp("/register");

    await user.type(await screen.findByLabelText("Email"), "dewi@example.com");
    await user.type(screen.getByLabelText("Kata sandi"), "kucing oranye di atap");
    await user.type(screen.getByLabelText("Nama Anda (opsional)"), "Dewi");
    await user.click(screen.getByRole("button", { name: "Lanjut" }));

    expect(await screen.findByRole("button", { name: "Tutup" })).toBeInTheDocument();
    expect(state.committed).toEqual({
      found: true,
      household_display_name: "Rumah tangga Dewi",
      pocket_display_name: "Tunai",
    });
    // The locale sent is the language the form was read in.
    expect(state.registered?.locale).toBe("id-ID");
    expect(noticeCard()).toHaveTextContent(
      "Kami menamai rumah tangga Anda Rumah tangga Dewi dan kantong pertama Anda Tunai, dengan mata uang IDR.",
    );
    expect(within(noticeCard()).getByRole("link", { name: "Buka Pengaturan" })).toBeInTheDocument();
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

describe("the Home notice", () => {
  async function founded(state: State) {
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    const view = renderApp("/register");
    await register(user, { name: "Dewi" });
    await screen.findByRole("button", { name: "Dismiss" });
    return { user, view };
  }

  it("is gone once dismissed, and a later visit to Home does not bring it back", async () => {
    const { user, view } = await founded(fresh());

    await user.click(screen.getByRole("button", { name: "Dismiss" }));
    expect(screen.queryByRole("button", { name: "Dismiss" })).not.toBeInTheDocument();

    await user.click(screen.getByRole("link", { name: "Settings" }));
    await screen.findByRole("heading", { name: "Settings" });
    act(() => view.navigate(-1));
    expect(await screen.findByRole("heading", { name: "Home" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Dismiss" })).not.toBeInTheDocument();
  });

  it("is gone once the person leaves Home, dismissed or not", async () => {
    const { user, view } = await founded(fresh());

    await user.click(screen.getByRole("link", { name: "Settings" }));
    await screen.findByRole("heading", { name: "Settings" });
    act(() => view.navigate(-1));

    expect(await screen.findByRole("heading", { name: "Home" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Dismiss" })).not.toBeInTheDocument();
  });

  it("is not shown on Home without the founding router state", async () => {
    server.use(...defaultHandlers({ signedIn: true, me: alice }));
    renderApp("/");
    expect(await screen.findByRole("heading", { name: "Home" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Dismiss" })).not.toBeInTheDocument();
    expect(screen.queryByText(/We named your Household/)).not.toBeInTheDocument();
  });

  it("is stored nowhere", async () => {
    await founded(fresh());
    // Nothing about the notice, whatever else the app keeps.
    const keys = [...Object.keys(window.localStorage), ...Object.keys(window.sessionStorage)];
    expect(keys.filter((k) => /found|notice/i.test(k))).toEqual([]);
    expect(JSON.stringify({ ...window.localStorage })).not.toMatch(/Cash|named/);
  });
});

describe("founding at /onboarding", () => {
  const handshake = { email: "d@example.com", user_display_name: "D", locale: "en-GB" };

  it("sends a visitor with no handshake to sign in", async () => {
    server.use(...defaultHandlers());
    renderApp("/onboarding");
    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
  });

  it("shows Setting up while the commit is in flight", async () => {
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    server.use(
      http.post("/api/onboarding/choice", async () => {
        await gate;
        return new HttpResponse(null, { status: 204 });
      }),
      ...defaultHandlers(fresh({ handshake })),
    );
    renderApp("/onboarding");
    expect(await screen.findByText("Setting up…")).toBeInTheDocument();
    release();
  });

  it("sends the person to sign in when the handshake lapses before the commit", async () => {
    server.use(
      http.post("/api/onboarding/choice", () =>
        HttpResponse.json(envelope("UNAUTHORIZED"), { status: 401 }),
      ),
      ...defaultHandlers(fresh({ handshake })),
    );
    renderApp("/onboarding");
    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
  });

  it("explains a closed instance when the commit answers FOUNDING_DISABLED", async () => {
    // The options said open; the operator closed it since (display only).
    server.use(
      http.post("/api/onboarding/choice", () =>
        HttpResponse.json(envelope("FOUNDING_DISABLED"), { status: 403 }),
      ),
      ...defaultHandlers(fresh({ handshake })),
    );
    renderApp("/onboarding");

    expect(await screen.findByText(/isn't accepting new Households/)).toBeInTheDocument();
    expect(screen.queryByText(/FOUNDING_DISABLED/)).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Back to sign in" })).toHaveAttribute("href", "/login");
  });

  it("explains up front, and commits nothing, when the options say founding is closed", async () => {
    const state = fresh({ foundingOpen: false, handshake });
    server.use(...defaultHandlers(state));
    renderApp("/onboarding");
    expect(await screen.findByText(/isn't accepting new Households/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
    expect(state.committed).toBeUndefined();
  });

  it("shows a failure in words, and Try again resends the same body", async () => {
    const state = fresh({ handshake });
    const sent: unknown[] = [];
    server.use(
      http.post(
        "/api/onboarding/choice",
        async ({ request }) => {
          sent.push(await request.json());
          return HttpResponse.json(envelope("INTERNAL"), { status: 500 });
        },
        { once: true },
      ),
      ...defaultHandlers(state),
    );
    const user = userEvent.setup();
    renderApp("/onboarding");

    expect(await screen.findByRole("alert")).toHaveTextContent(enGB.errors.INTERNAL);
    expect(screen.queryByText(/INTERNAL/)).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Try again" }));

    expect(await screen.findByRole("heading", { name: "Home" })).toBeInTheDocument();
    expect(sent).toEqual([
      { found: true, household_display_name: "D's Household", pocket_display_name: "Cash" },
    ]);
    expect(state.committed).toEqual(sent[0]);
  });

  it("says the address was taken at the commit, and offers sign-in", async () => {
    server.use(
      http.post("/api/onboarding/choice", () =>
        HttpResponse.json(envelope("EMAIL_TAKEN"), { status: 409 }),
      ),
      ...defaultHandlers(fresh({ handshake })),
    );
    renderApp("/onboarding");

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("An account with that email already exists.");
    expect(within(alert).getByRole("link", { name: "Sign in" })).toHaveAttribute("href", "/login");
  });

  it("leaves for the shell when already signed in, without founding", async () => {
    const state: State = { signedIn: true, me: alice };
    server.use(...defaultHandlers(state));
    renderApp("/onboarding");
    expect(await screen.findByRole("heading", { name: "Home" })).toBeInTheDocument();
    expect(state.committed).toBeUndefined();
    expect(screen.queryByRole("button", { name: "Dismiss" })).not.toBeInTheDocument();
  });
});

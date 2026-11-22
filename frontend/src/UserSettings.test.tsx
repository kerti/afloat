import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { toast } from "sonner";
import { alice, defaultHandlers, envelope, server, type State } from "@/test/server";
import { renderApp } from "@/test/render";

vi.mock("sonner", async () => (await import("@/test/sonnerMock")).sonnerMock());
const toastMock = vi.mocked(toast);
beforeEach(() => {
  vi.clearAllMocks();
});

const signedIn = (extra: Partial<State> = {}): State => ({ signedIn: true, me: alice, ...extra });

const profile = () => within(screen.getByRole("region", { name: "Profile" }));
const email = () => within(screen.getByRole("region", { name: "Email" }));
const password = () => within(screen.getByRole("region", { name: "Password" }));

// Language is a Radix Select: open it, then pick the option.
async function chooseLanguage(user: ReturnType<typeof userEvent.setup>, name: string) {
  await user.click(profile().getByRole("combobox", { name: "Language" }));
  await user.click(await screen.findByRole("option", { name }));
}

async function open() {
  await screen.findByRole("region", { name: "Profile" });
  await screen.findByRole("region", { name: "Password" });
}

describe("user settings", () => {
  it("is reached from the shell and shows the current values", async () => {
    server.use(...defaultHandlers(signedIn()));
    const user = userEvent.setup();
    renderApp("/");

    await user.click(await screen.findByRole("link", { name: "Your settings" }));
    await open();

    expect(profile().getByLabelText("Your name")).toHaveValue("Alice");
    expect(profile().getByRole("combobox", { name: "Language" })).toHaveTextContent("English (UK)");
    expect(profile().getByLabelText("Time zone")).toHaveValue("Asia/Jakarta");
    expect(profile().getByRole("button", { name: "Save" })).toBeDisabled();
    expect(password().getByText(/signs you out everywhere else/)).toBeInTheDocument();
  });

  it("offers the browser's zones plus the stored one when the browser lacks it", async () => {
    const odd = { ...alice, time_zone: "Mars/Base" };
    server.use(...defaultHandlers(signedIn({ me: odd })));
    renderApp("/settings/user");
    await open();

    const select = profile().getByLabelText("Time zone");
    expect(select).toHaveValue("Mars/Base");
    const offered = within(select)
      .getAllByRole("option")
      .map((o) => o.getAttribute("value"));
    expect(offered).toContain("Asia/Makassar");
    expect(offered).toContain("Mars/Base");
  });

  it("sends only the name when only the name changed, raises one toast, and the shell shows it after", async () => {
    const state = signedIn();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/settings/user");
    await open();

    const field = profile().getByLabelText("Your name");
    await user.clear(field);
    await user.type(field, "Alicia");
    await user.click(profile().getByRole("button", { name: "Save" }));

    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    expect(toastMock).toHaveBeenCalledWith("Saved.", expect.objectContaining({ duration: 4000 }));
    expect(state.mePatches).toEqual([{ user_display_name: "Alicia" }]);

    await user.click(screen.getByRole("link", { name: "Home" }));
    expect(await screen.findByText("Signed in as Alicia.")).toBeInTheDocument();
  });

  it("saves the chosen zone on change, with one toast, and nothing else", async () => {
    const state = signedIn();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/settings/user");
    await open();

    await user.selectOptions(profile().getByLabelText("Time zone"), "Asia/Makassar");

    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    expect(toastMock).toHaveBeenCalledWith("Saved.", expect.objectContaining({ duration: 4000 }));
    expect(state.mePatches).toEqual([{ time_zone: "Asia/Makassar" }]);
    expect(profile().getByLabelText("Time zone")).toHaveValue("Asia/Makassar");
  });

  it("flips the UI language when the language is chosen", async () => {
    const state = signedIn();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/settings/user");
    await open();

    await chooseLanguage(user, "Bahasa Indonesia");

    expect(await screen.findByRole("heading", { name: "Pengaturan Anda" })).toBeInTheDocument();
    expect(state.mePatches).toEqual([{ locale: "id-ID" }]);
    expect(screen.getByRole("region", { name: "Profil" })).toBeInTheDocument();
    await waitFor(() =>
      expect(toastMock).toHaveBeenCalledWith(
        "Tersimpan.",
        expect.objectContaining({ duration: 4000 }),
      ),
    );
  });

  it("shows a VALIDATION on the name inline, in words, and marks the field", async () => {
    server.use(
      http.patch("/api/me", () =>
        HttpResponse.json(
          envelope("VALIDATION", { field: "user_display_name", rule: "required" }),
          {
            status: 400,
          },
        ),
      ),
      ...defaultHandlers(signedIn()),
    );
    const user = userEvent.setup();
    renderApp("/settings/user");
    await open();

    await user.type(profile().getByLabelText("Your name"), "x");
    await user.click(profile().getByRole("button", { name: "Save" }));

    expect(await profile().findByRole("alert")).toHaveTextContent("Check the name field.");
    expect(profile().getByLabelText("Your name")).toHaveAttribute("aria-invalid", "true");
    expect(toastMock).not.toHaveBeenCalled();
  });

  it("reverts the name on Esc", async () => {
    server.use(...defaultHandlers(signedIn()));
    const user = userEvent.setup();
    renderApp("/settings/user");
    await open();

    await user.type(profile().getByLabelText("Your name"), "x");
    await user.keyboard("{Escape}");
    expect(profile().getByLabelText("Your name")).toHaveValue("Alice");
    expect(profile().getByRole("button", { name: "Save" })).toBeDisabled();
  });

  it.each([
    ["time_zone", "Check the time zone field."],
    ["locale", "Check the language field."],
  ])("rolls a failed %s choice back and says why in an error toast", async (field, text) => {
    server.use(
      http.patch("/api/me", () =>
        HttpResponse.json(envelope("VALIDATION", { field, rule: "required" }), { status: 400 }),
      ),
      ...defaultHandlers(signedIn()),
    );
    const user = userEvent.setup();
    renderApp("/settings/user");
    await open();

    if (field === "time_zone") {
      await user.selectOptions(profile().getByLabelText("Time zone"), "Asia/Makassar");
    } else {
      await chooseLanguage(user, "Bahasa Indonesia");
    }

    await waitFor(() => expect(toastMock.error).toHaveBeenCalledWith(text, expect.anything()));
    expect(profile().getByLabelText("Time zone")).toHaveValue("Asia/Jakarta");
    expect(profile().getByRole("combobox", { name: "Language" })).toHaveTextContent("English (UK)");
    expect(toastMock).not.toHaveBeenCalled();
  });

  it.each([
    ["en-GB", "time_zone", "Check the time zone field."],
    ["en-GB", "user_display_name", "Check the name field."],
    ["id-ID", "time_zone", "Periksa kolom zona waktu."],
    ["id-ID", "user_display_name", "Periksa kolom nama."],
  ] as const)("words a %s VALIDATION on %s", async (locale, field, text) => {
    server.use(
      http.patch("/api/me", () =>
        HttpResponse.json(envelope("VALIDATION", { field, rule: "required" }), { status: 400 }),
      ),
      ...defaultHandlers(signedIn({ me: { ...alice, locale } })),
    );
    const user = userEvent.setup();
    renderApp("/settings/user");
    const [nameLabel, saveLabel] =
      locale === "id-ID" ? ["Nama Anda", "Simpan"] : ["Your name", "Save"];
    await screen.findByLabelText(nameLabel);

    await user.type(screen.getByLabelText(nameLabel), "x");
    await user.click(screen.getAllByRole("button", { name: saveLabel })[0]!);

    expect(await screen.findByRole("alert")).toHaveTextContent(text);
  });

  it("changes the email, shows it in the shell and says which address to sign in with", async () => {
    const state = signedIn();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    const { client } = renderApp("/settings/user");
    await open();

    await user.type(email().getByLabelText("New email"), "alice@new.example");
    await user.type(email().getByLabelText("Current password"), "correct-horse");
    await user.click(email().getByRole("button", { name: "Change email" }));

    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    expect(toastMock).toHaveBeenCalledWith(
      "Saved. Sign in with alice@new.example from now on.",
      expect.objectContaining({
        duration: 4000,
      }),
    );
    expect(state.emailChanges).toEqual([
      { email: "alice@new.example", current_password: "correct-horse" },
    ]);
    expect(email().getByLabelText("Current password")).toHaveValue("");
    expect(email().getByLabelText("New email")).toHaveValue("");
    // /me was invalidated and refetched, so the cache holds the new address.
    await waitFor(() =>
      expect(client.getQueryData<{ email: string }>(["me"])?.email).toBe("alice@new.example"),
    );
  });

  it("keeps a wrong current password on the email form and does not end the session", async () => {
    server.use(...defaultHandlers(signedIn()));
    const user = userEvent.setup();
    const { client } = renderApp("/settings/user");
    await open();

    await user.type(email().getByLabelText("New email"), "alice@new.example");
    await user.type(email().getByLabelText("Current password"), "wrong");
    await user.click(email().getByRole("button", { name: "Change email" }));

    expect(await email().findByRole("alert")).toHaveTextContent(
      "That isn't your current password.",
    );
    expect(email().getByLabelText("Current password")).toHaveAttribute("aria-invalid", "true");
    expect(email().getByLabelText("Current password")).toHaveValue("");
    expect(profile().queryByRole("alert")).not.toBeInTheDocument();
    expect(client.getQueryData(["me"])).not.toBeNull();
  });

  it("shows EMAIL_TAKEN on the email form and marks the address", async () => {
    server.use(
      http.post("/api/auth/local/change-email", () =>
        HttpResponse.json(envelope("EMAIL_TAKEN"), { status: 409 }),
      ),
      ...defaultHandlers(signedIn()),
    );
    const user = userEvent.setup();
    const { client } = renderApp("/settings/user");
    await open();

    await user.type(email().getByLabelText("New email"), "bob@example.com");
    await user.type(email().getByLabelText("Current password"), "correct-horse");
    await user.click(email().getByRole("button", { name: "Change email" }));

    expect(await email().findByRole("alert")).toHaveTextContent(
      "An account with that email already exists.",
    );
    expect(email().getByLabelText("New email")).toHaveAttribute("aria-invalid", "true");
    expect(client.getQueryData(["me"])).not.toBeNull();
  });

  it("shows the wait on a 429", async () => {
    server.use(
      http.post("/api/auth/local/change-email", () =>
        HttpResponse.json(envelope("TOO_MANY_ATTEMPTS"), {
          status: 429,
          headers: { "Retry-After": "30" },
        }),
      ),
      ...defaultHandlers(signedIn()),
    );
    const user = userEvent.setup();
    renderApp("/settings/user");
    await open();

    await user.type(email().getByLabelText("New email"), "bob@example.com");
    await user.type(email().getByLabelText("Current password"), "x");
    await user.click(email().getByRole("button", { name: "Change email" }));

    expect(await email().findByRole("alert")).toHaveTextContent(
      "Too many attempts. Try again in 30 seconds.",
    );
  });

  it("changes the password with the new one on the wire and no confirm field", async () => {
    const state = signedIn();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/settings/user");
    await open();

    await user.type(password().getByLabelText("Current password"), "correct-horse");
    await user.type(password().getByLabelText("New password"), "a much longer phrase");
    await user.type(password().getByLabelText("Confirm new password"), "a much longer phrase");
    await user.click(password().getByRole("button", { name: "Change password" }));

    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    expect(toastMock).toHaveBeenCalledWith(
      "Password changed.",
      expect.objectContaining({ duration: 4000 }),
    );
    expect(state.passwordChanges).toEqual([
      { current_password: "correct-horse", new_password: "a much longer phrase" },
    ]);
    expect(password().getByLabelText("New password")).toHaveValue("");
    expect(password().getByLabelText("Current password")).toHaveValue("");
  });

  it("sends nothing when the confirm field does not match", async () => {
    const state = signedIn();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/settings/user");
    await open();

    await user.type(password().getByLabelText("Current password"), "correct-horse");
    await user.type(password().getByLabelText("New password"), "a much longer phrase");
    await user.type(password().getByLabelText("Confirm new password"), "a much longer phrasX");
    await user.click(password().getByRole("button", { name: "Change password" }));

    expect(await password().findByRole("alert")).toHaveTextContent(
      "The new passwords don't match.",
    );
    expect(password().getByLabelText("Confirm new password")).toHaveAttribute(
      "aria-invalid",
      "true",
    );
    expect(state.passwordChanges).toBeUndefined();
  });

  it("keeps a wrong current password on the password form and does not end the session", async () => {
    server.use(...defaultHandlers(signedIn()));
    const user = userEvent.setup();
    const { client } = renderApp("/settings/user");
    await open();

    await user.type(password().getByLabelText("Current password"), "wrong");
    await user.type(password().getByLabelText("New password"), "a much longer phrase");
    await user.type(password().getByLabelText("Confirm new password"), "a much longer phrase");
    await user.click(password().getByRole("button", { name: "Change password" }));

    expect(await password().findByRole("alert")).toHaveTextContent(
      "That isn't your current password.",
    );
    expect(password().getByLabelText("Current password")).toHaveAttribute("aria-invalid", "true");
    expect(client.getQueryData(["me"])).not.toBeNull();
  });

  it.each([
    ["min", "Use at least 10 characters."],
    ["breached", "That password is too common. Pick another."],
  ])("shows a WEAK_PASSWORD with reason %s", async (reason, text) => {
    server.use(
      http.post("/api/auth/local/change-password", () =>
        HttpResponse.json(envelope("WEAK_PASSWORD", { reason }), { status: 400 }),
      ),
      ...defaultHandlers(signedIn()),
    );
    const user = userEvent.setup();
    renderApp("/settings/user");
    await open();

    await user.type(password().getByLabelText("Current password"), "correct-horse");
    await user.type(password().getByLabelText("New password"), "short");
    await user.type(password().getByLabelText("Confirm new password"), "short");
    await user.click(password().getByRole("button", { name: "Change password" }));

    expect(await password().findByRole("alert")).toHaveTextContent(text);
    expect(password().getByLabelText("New password")).toHaveAttribute("aria-invalid", "true");
  });

  it("cancels a /me refetch in flight, so the old cookie cannot end the session", async () => {
    const state = signedIn();
    let release!: () => void;
    const held = new Promise<void>((resolve) => (release = resolve));
    let meCalls = 0;
    server.use(
      // After the first read, /me is held and then answers as the old cookie
      // would once the server has rotated it.
      http.get("/api/me", async () => {
        if (++meCalls === 1) return HttpResponse.json(state.me);
        await held;
        return HttpResponse.json(envelope("UNAUTHORIZED"), { status: 401 });
      }),
      ...defaultHandlers(state),
    );
    const user = userEvent.setup();
    const { client } = renderApp("/settings/user");
    await open();

    void client.invalidateQueries({ queryKey: ["me"] });
    await waitFor(() => expect(meCalls).toBe(2));

    await user.type(password().getByLabelText("Current password"), "correct-horse");
    await user.type(password().getByLabelText("New password"), "a much longer phrase");
    await user.type(password().getByLabelText("Confirm new password"), "a much longer phrase");
    await user.click(password().getByRole("button", { name: "Change password" }));
    await waitFor(() =>
      expect(toastMock).toHaveBeenCalledWith(
        "Password changed.",
        expect.objectContaining({ duration: 4000 }),
      ),
    );

    release();
    await new Promise((r) => setTimeout(r, 50));

    expect(screen.queryByRole("heading", { name: "Sign in" })).not.toBeInTheDocument();
    expect(client.getQueryData(["me"])).toEqual(state.me);
    expect(screen.getByRole("heading", { name: "Your settings" })).toBeInTheDocument();
  });

  it("sends a person whose session ended elsewhere to sign in", async () => {
    const state = signedIn();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    const { client } = renderApp("/settings/user");
    await open();

    // Another device changed the password: this cookie is dead, so the next
    // /me probe answers 401 and the UI drops to sign-in.
    state.signedIn = false;
    await user.type(profile().getByLabelText("Your name"), "x");
    await user.click(profile().getByRole("button", { name: "Save" }));

    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
    expect(client.getQueryData(["me"])).toBeNull();
  });

  it("sends the person to sign in when a write answers 401 UNAUTHORIZED", async () => {
    server.use(
      http.post("/api/auth/local/change-password", () =>
        HttpResponse.json(envelope("UNAUTHORIZED"), { status: 401 }),
      ),
      ...defaultHandlers(signedIn()),
    );
    const user = userEvent.setup();
    renderApp("/settings/user");
    await open();

    await user.type(password().getByLabelText("Current password"), "correct-horse");
    await user.type(password().getByLabelText("New password"), "a much longer phrase");
    await user.type(password().getByLabelText("Confirm new password"), "a much longer phrase");
    await user.click(password().getByRole("button", { name: "Change password" }));

    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
  });

  it("shows a network failure instead of swallowing it", async () => {
    server.use(
      http.patch("/api/me", () => HttpResponse.error()),
      ...defaultHandlers(signedIn()),
    );
    const user = userEvent.setup();
    renderApp("/settings/user");
    await open();

    await user.type(profile().getByLabelText("Your name"), "x");
    await user.click(profile().getByRole("button", { name: "Save" }));

    expect(await profile().findByRole("alert")).toHaveTextContent("Can't reach Afloat.");
  });

  it("leaves out the email and password forms when local sign-in is off", async () => {
    server.use(
      http.get("/api/auth/methods", () =>
        HttpResponse.json({ local: false, google: true, founding_open: true }),
      ),
      ...defaultHandlers(signedIn()),
    );
    renderApp("/settings/user");

    await screen.findByRole("region", { name: "Profile" });
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.queryByRole("region", { name: "Email" })).not.toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Password" })).not.toBeInTheDocument();
  });

  it("sends a signed-out visitor to sign in", async () => {
    server.use(...defaultHandlers());
    renderApp("/settings/user");
    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
  });
});

import { describe, expect, it } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { delay, http, HttpResponse } from "msw";
import { alice, defaultHandlers, envelope, server } from "@/test/server";
import { renderApp } from "@/test/render";
import { i18n } from "@/i18n";

describe("session flow", () => {
  it("sends a signed-out visitor to the login screen", async () => {
    server.use(...defaultHandlers());
    renderApp("/");
    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
  });

  it("signs in, shows the user and household, and signs out again", async () => {
    server.use(...defaultHandlers());
    const user = userEvent.setup();
    renderApp("/login");

    await user.type(await screen.findByLabelText("Email"), "alice@example.com");
    await user.type(screen.getByLabelText("Password"), "correct-horse");
    await user.click(screen.getByRole("button", { name: "Sign in" }));

    expect(await screen.findByText("Signed in as Alice")).toBeInTheDocument();
    expect(screen.getByText("Household: Test Household")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Sign out" }));
    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
  });

  it("shows INVALID_CREDENTIALS in the user's words, never the code", async () => {
    server.use(...defaultHandlers());
    const user = userEvent.setup();
    renderApp("/login");

    await user.type(await screen.findByLabelText("Email"), "alice@example.com");
    await user.type(screen.getByLabelText("Password"), "wrong");
    await user.click(screen.getByRole("button", { name: "Sign in" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "That email and password don't match.",
    );
    expect(screen.queryByText(/INVALID_CREDENTIALS/)).not.toBeInTheDocument();
  });

  it("reads the backoff window from Retry-After", async () => {
    server.use(
      http.post("/api/auth/local/login", () =>
        HttpResponse.json(envelope("TOO_MANY_ATTEMPTS"), {
          status: 429,
          headers: { "Retry-After": "4" },
        }),
      ),
      ...defaultHandlers(),
    );
    const user = userEvent.setup();
    renderApp("/login");

    await user.type(await screen.findByLabelText("Email"), "alice@example.com");
    await user.type(screen.getByLabelText("Password"), "x");
    await user.click(screen.getByRole("button", { name: "Sign in" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Try again in 4 seconds.");
  });

  it("says so, visibly, when the server cannot be reached", async () => {
    server.use(
      http.post("/api/auth/local/login", () => HttpResponse.error()),
      ...defaultHandlers(),
    );
    const user = userEvent.setup();
    renderApp("/login");

    await user.type(await screen.findByLabelText("Email"), "alice@example.com");
    await user.type(screen.getByLabelText("Password"), "x");
    await user.click(screen.getByRole("button", { name: "Sign in" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Can't reach Afloat.");
  });

  it("hides the form when local sign-in is turned off", async () => {
    server.use(
      http.get("/api/auth/methods", () => HttpResponse.json({ local: false, google: true })),
      ...defaultHandlers(),
    );
    renderApp("/login");
    expect(await screen.findByText(/Password sign-in is turned off/)).toBeInTheDocument();
    expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
  });

  it("shows a /me failure instead of treating it as signed out", async () => {
    server.use(
      http.get("/api/me", () => HttpResponse.json(envelope("INTERNAL"), { status: 500 })),
      ...defaultHandlers(),
    );
    renderApp("/");
    expect(await screen.findByRole("alert")).toHaveTextContent("Afloat couldn't complete that.");
  });

  it("switches to the signed-in user's own locale, <html lang> included", async () => {
    server.use(...defaultHandlers({ signedIn: true, me: { ...alice, locale: "id-ID" } }));
    renderApp("/");
    expect(await screen.findByText("Masuk sebagai Alice")).toBeInTheDocument();
    expect(document.documentElement.lang).toBe("id-ID");
  });

  it("drops the last user's locale on sign-out", async () => {
    server.use(...defaultHandlers({ signedIn: true, me: { ...alice, locale: "id-ID" } }));
    const user = userEvent.setup();
    renderApp("/");
    await user.click(await screen.findByRole("button", { name: "Keluar" }));
    // jsdom's navigator.languages is en-US, so the browser's choice is en-GB.
    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
    expect(i18n.language).toBe("en-GB");
  });

  it("leaves nothing of the last user in the cache on sign-out", async () => {
    server.use(...defaultHandlers({ signedIn: true, me: alice }));
    const user = userEvent.setup();
    const { client } = renderApp("/");
    await screen.findByText("Signed in as Alice");
    // Stands in for the Household data later steps will cache.
    client.setQueryData(["household", "expenses"], [{ id: "x" }]);

    await user.click(screen.getByRole("button", { name: "Sign out" }));
    await screen.findByRole("heading", { name: "Sign in" });

    expect(client.getQueryData(["household", "expenses"])).toBeUndefined();
    expect(client.getQueryData(["me"])).toBeNull();
  });

  it("does not let a /me answer still in flight sign the last user back in", async () => {
    let slow = false;
    server.use(
      http.get("/api/me", async () => {
        // Answered as of before sign-out, arriving after it.
        if (slow) await delay(150);
        return HttpResponse.json(alice);
      }),
      ...defaultHandlers(),
    );
    const user = userEvent.setup();
    const { client } = renderApp("/");
    await screen.findByText("Signed in as Alice");

    slow = true;
    void client.invalidateQueries({ queryKey: ["me"] });
    await user.click(screen.getByRole("button", { name: "Sign out" }));
    await screen.findByRole("heading", { name: "Sign in" });

    await new Promise((r) => setTimeout(r, 300));
    expect(client.getQueryData(["me"])).toBeNull();
    expect(screen.getByRole("heading", { name: "Sign in" })).toBeInTheDocument();
  });

  it("offers a retry when /me fails, and recovers", async () => {
    let fail = true;
    server.use(
      http.get("/api/me", () =>
        fail ? HttpResponse.json(envelope("INTERNAL"), { status: 500 }) : HttpResponse.json(alice),
      ),
      ...defaultHandlers(),
    );
    const user = userEvent.setup();
    renderApp("/");
    await screen.findByRole("alert");
    fail = false;
    await user.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("Signed in as Alice")).toBeInTheDocument();
  });

  it("leaves the login screen for home when already signed in", async () => {
    server.use(...defaultHandlers({ signedIn: true, me: alice }));
    renderApp("/login");
    await waitFor(() => expect(screen.getByText("Signed in as Alice")).toBeInTheDocument());
  });
});

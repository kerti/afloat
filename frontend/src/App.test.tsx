import { describe, expect, it } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
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

  it("switches to the signed-in user's own locale", async () => {
    server.use(...defaultHandlers({ signedIn: true, me: { ...alice, locale: "id-ID" } }));
    renderApp("/");
    expect(await screen.findByText("Masuk sebagai Alice")).toBeInTheDocument();
    await i18n.changeLanguage("en-GB");
  });

  it("leaves the login screen for home when already signed in", async () => {
    server.use(...defaultHandlers({ signedIn: true, me: alice }));
    renderApp("/login");
    await waitFor(() => expect(screen.getByText("Signed in as Alice")).toBeInTheDocument());
  });
});

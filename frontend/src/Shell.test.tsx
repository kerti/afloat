import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { toast } from "sonner";
import { alice, defaultHandlers, envelope, server } from "@/test/server";
import { renderApp } from "@/test/render";
import enGB from "@/locales/en-GB.json";

vi.mock("sonner", async () => (await import("@/test/sonnerMock")).sonnerMock());
const toastMock = vi.mocked(toast);

beforeEach(() => {
  vi.clearAllMocks();
});

describe("the Shell", () => {
  it("frames every signed-in screen: a fixed top bar, one max-w-xl column, no bottom bar", async () => {
    server.use(...defaultHandlers({ signedIn: true, me: alice }));
    const user = userEvent.setup();
    renderApp("/");

    const bar = await screen.findByRole("banner");
    expect(bar).toHaveClass("fixed");
    expect(within(bar).getByRole("link", { name: "Afloat" })).toHaveAttribute("href", "/");
    expect(within(bar).getByRole("button", { name: enGB.shell.signOut })).toBeInTheDocument();
    expect(screen.queryByRole("navigation")).not.toBeInTheDocument();

    for (const link of ["Pockets", "Household settings", "Your settings"]) {
      await user.click(await screen.findByRole("link", { name: link }));
      expect(screen.getByRole("main").firstElementChild).toHaveClass("max-w-xl");
      expect(screen.getAllByRole("heading", { level: 1 })).toHaveLength(1);
      await user.click(screen.getByRole("link", { name: enGB.home.title }));
    }
  });

  it("gives a sub-screen a BackLink to Home, at least 44px tall, above the title", async () => {
    server.use(...defaultHandlers({ signedIn: true, me: alice }));
    renderApp("/pockets");

    const back = await screen.findByRole("link", { name: enGB.home.title });
    expect(back).toHaveAttribute("href", "/");
    expect(back).toHaveClass("min-h-11");
    const heading = screen.getByRole("heading", { level: 1 });
    expect(back.compareDocumentPosition(heading) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("leaves auth screens outside it, in a max-w-sm card", async () => {
    server.use(...defaultHandlers());
    renderApp("/login");
    await screen.findByRole("heading", { name: "Sign in" });
    expect(screen.getByRole("main")).toHaveClass("max-w-sm");
    expect(screen.queryByRole("banner")).not.toBeInTheDocument();
  });

  it("shows a failed sign-out as an error toast, in words", async () => {
    server.use(
      http.post("/api/auth/logout", () => HttpResponse.json(envelope("INTERNAL"), { status: 500 })),
      ...defaultHandlers({ signedIn: true, me: alice }),
    );
    const user = userEvent.setup();
    renderApp("/");

    await user.click(await screen.findByRole("button", { name: enGB.shell.signOut }));

    await waitFor(() =>
      expect(toastMock.error).toHaveBeenCalledWith(
        "Afloat couldn't complete that. Try again in a moment.",
        expect.anything(),
      ),
    );
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("shows the session probe's loading and failure states, not bare text", async () => {
    server.use(
      http.get("/api/me", () => HttpResponse.json(envelope("INTERNAL"), { status: 500 })),
      ...defaultHandlers(),
    );
    renderApp("/");
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveClass("text-attention");
    expect(within(alert).getByRole("button", { name: enGB.app.retry })).toHaveClass("w-full");
  });
});

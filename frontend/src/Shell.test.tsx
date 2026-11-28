import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { toast } from "sonner";
import { alice, defaultHandlers, envelope, server } from "@/test/server";
import { renderApp } from "@/test/render";
import enGB from "@/locales/en-GB.json";
import idID from "@/locales/id-ID.json";

vi.mock("sonner", async () => (await import("@/test/sonnerMock")).sonnerMock());
const toastMock = vi.mocked(toast);

beforeEach(() => {
  vi.clearAllMocks();
});

describe("the Shell", () => {
  it("frames every signed-in screen: a fixed top bar, one max-w-xl column, a fixed bottom bar", async () => {
    server.use(...defaultHandlers({ signedIn: true, me: alice }));
    const user = userEvent.setup();
    renderApp("/");

    const bar = await screen.findByRole("banner");
    expect(bar).toHaveClass("fixed");
    expect(within(bar).getByRole("link", { name: "Afloat" })).toHaveAttribute("href", "/");
    // The build under the name; Sign out is not in the bar but ends Settings.
    expect(within(bar).getByText("dev (test)")).toBeInTheDocument();
    expect(within(bar).queryByRole("button", { name: enGB.shell.signOut })).not.toBeInTheDocument();
    const tabs = screen.getByRole("navigation", { name: enGB.shell.mainNav });
    expect(tabs).toHaveClass("fixed");

    await user.click(await screen.findByRole("link", { name: "Settings" }));
    expect(screen.getByRole("main").firstElementChild).toHaveClass("max-w-xl");
    expect(await screen.findByRole("button", { name: enGB.shell.signOut })).toBeInTheDocument();
    expect(screen.getAllByRole("heading", { level: 1 })).toHaveLength(1);
  });

  it("gives a tab screen no back link to Home", async () => {
    server.use(...defaultHandlers({ signedIn: true, me: alice }));
    renderApp("/settings");

    await screen.findByRole("heading", { level: 1 });
    expect(
      within(screen.getByRole("main")).queryByRole("link", { name: enGB.home.title }),
    ).toBeNull();
  });

  it("has five tabs in order, each an icon over a label, always visible", async () => {
    server.use(...defaultHandlers({ signedIn: true, me: alice }));
    renderApp("/");

    const tabs = await screen.findByRole("navigation", { name: enGB.shell.mainNav });
    const links = within(tabs).getAllByRole("link");
    expect(links.map((l) => l.textContent)).toEqual([
      "Home",
      "Expenses",
      "Capture",
      "Budget",
      "Settings",
    ]);
    expect(links.map((l) => l.getAttribute("href"))).toEqual([
      "/",
      "/expenses",
      "/capture",
      "/budget",
      "/settings",
    ]);
    for (const link of links)
      expect(link.querySelector("svg")).toHaveAttribute("aria-hidden", "true");
  });

  it.each([
    ["/", "Home"],
    ["/expenses", "Expenses"],
    ["/capture", "Capture"],
    ["/budget", "Budget"],
    ["/settings", "Settings"],
  ])("lights only the tab for %s", async (path, label) => {
    server.use(...defaultHandlers({ signedIn: true, me: alice }));
    renderApp(path);

    const tabs = await screen.findByRole("navigation", { name: enGB.shell.mainNav });
    const current = within(tabs)
      .getAllByRole("link")
      .filter((l) => l.getAttribute("aria-current") === "page");
    expect(current.map((l) => l.textContent)).toEqual([label]);
  });

  it("lights a tab across its own sub-paths, and Home only on /", async () => {
    server.use(...defaultHandlers({ signedIn: true, me: alice }));
    const user = userEvent.setup();
    renderApp("/settings");

    const tabs = await screen.findByRole("navigation", { name: enGB.shell.mainNav });
    expect(within(tabs).getByRole("link", { name: "Home" })).not.toHaveAttribute("aria-current");
    await user.click(within(tabs).getByRole("link", { name: "Home" }));
    expect(within(tabs).getByRole("link", { name: "Home" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(within(tabs).getByRole("link", { name: "Settings" })).not.toHaveAttribute(
      "aria-current",
    );
  });

  it("opens a new screen at its top, and a tap on the tab already showing reloads it", async () => {
    server.use(...defaultHandlers({ signedIn: true, me: alice }));
    const scrollTo = vi.spyOn(window, "scrollTo").mockImplementation(() => {});
    let dashboards = 0;
    const count = ({ request }: { request: Request }) => {
      if (new URL(request.url).pathname === "/api/dashboard") dashboards++;
    };
    server.events.on("request:start", count);
    const user = userEvent.setup();
    renderApp("/");
    const tabs = within(await screen.findByRole("navigation", { name: enGB.shell.mainNav }));
    await waitFor(() => expect(dashboards).toBe(1));

    await user.click(tabs.getByRole("link", { name: enGB.shell.tabs.settings }));
    expect(scrollTo).toHaveBeenCalledWith(0, 0);
    await user.click(tabs.getByRole("link", { name: enGB.shell.tabs.home }));
    await waitFor(() => expect(dashboards).toBe(2));

    scrollTo.mockClear();
    await user.click(tabs.getByRole("link", { name: enGB.shell.tabs.home }));
    expect(scrollTo).toHaveBeenCalledWith(0, 0);
    await waitFor(() => expect(dashboards).toBe(3));
    expect(screen.getByTestId("location").textContent).toBe("/");

    server.events.removeListener("request:start", count);
    scrollTo.mockRestore();
  });

  it("paints Capture's label over its raised square's shadow, as bright as the other labels", async () => {
    server.use(...defaultHandlers({ signedIn: true, me: alice }));
    renderApp("/");
    const tabs = within(await screen.findByRole("navigation", { name: enGB.shell.mainNav }));
    expect(tabs.getByText(enGB.shell.tabs.capture)).toHaveClass("relative");
  });

  it("words the bar in Indonesian", async () => {
    server.use(...defaultHandlers({ signedIn: true, me: { ...alice, locale: "id-ID" } }));
    renderApp("/");

    const tabs = await screen.findByRole("navigation", { name: idID.shell.mainNav });
    expect(
      within(tabs)
        .getAllByRole("link")
        .map((l) => l.textContent),
    ).toEqual(["Beranda", "Pengeluaran", "Catat", "Anggaran", "Pengaturan"]);
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
    renderApp("/settings");

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

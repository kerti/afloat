import { describe, expect, it } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { alice, defaultHandlers, server, type State } from "@/test/server";
import { renderApp } from "@/test/render";

const signedIn = (extra: Partial<State> = {}): State => ({ signedIn: true, me: alice, ...extra });
const pocket = (id: string, name: string) => ({ id, pocket_display_name: name, active: true });

describe("the Settings screen", () => {
  it("is one screen: three h2 sections in order, and the You subsections as h3", async () => {
    server.use(...defaultHandlers(signedIn()));
    renderApp("/settings");

    await screen.findByRole("region", { name: "Password" });
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Settings");
    expect(screen.getAllByRole("heading", { level: 2 }).map((h) => h.textContent)).toEqual([
      "Household",
      "Pockets",
      "You",
    ]);
    expect(screen.getAllByRole("heading", { level: 3 }).map((h) => h.textContent)).toEqual([
      "Profile",
      "Email",
      "Password",
    ]);
    // Each subsection sits inside the You section.
    const you = screen.getByRole("region", { name: "You" });
    for (const name of ["Profile", "Email", "Password"]) {
      expect(within(you).getByRole("region", { name })).toBeInTheDocument();
    }
    expect(screen.getByRole("link", { name: "Home" })).toHaveAttribute("href", "/");
  });

  it("words its sections in Indonesian", async () => {
    server.use(...defaultHandlers(signedIn({ me: { ...alice, locale: "id-ID" } })));
    renderApp("/settings");

    await screen.findByRole("region", { name: "Kata sandi" });
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Pengaturan");
    expect(screen.getAllByRole("heading", { level: 2 }).map((h) => h.textContent)).toEqual([
      "Rumah tangga",
      "Kantong",
      "Anda",
    ]);
  });
});

describe("the old settings paths", () => {
  it.each(["/settings/household", "/settings/user", "/pockets"])(
    "%s redirects to /settings",
    async (old) => {
      server.use(...defaultHandlers(signedIn()));
      const view = renderApp(old);

      expect(await screen.findByRole("heading", { level: 1, name: "Settings" })).toBeVisible();
      expect(view.getByTestId("location")).toHaveTextContent(/^\/settings$/);
    },
  );

  it("keeps the query string, so /pockets?edit=pocket:<id> opens the edit dialog", async () => {
    server.use(
      ...defaultHandlers(signedIn({ pockets: [pocket("a", "Cash"), pocket("b", "OVO")] })),
    );
    const view = renderApp("/pockets?edit=pocket:b");

    const dialog = await screen.findByRole("dialog", { name: "Edit Pocket" });
    expect(within(dialog).getByLabelText("Pocket name")).toHaveValue("OVO");
    expect(view.getByTestId("location")).toHaveTextContent(/^\/settings\?edit=pocket:b$/);
  });

  it("keeps ?new=pocket on /settings/household", async () => {
    server.use(...defaultHandlers(signedIn()));
    const view = renderApp("/settings/household?new=pocket");

    expect(await screen.findByRole("dialog", { name: "Add a Pocket" })).toBeInTheDocument();
    expect(view.param("new")).toBe("pocket");
  });
});

describe("Home", () => {
  it("links to Settings, once, and no longer to the three old screens", async () => {
    server.use(...defaultHandlers(signedIn()));
    const user = userEvent.setup();
    renderApp("/");

    const link = await screen.findByRole("link", { name: "Settings" });
    expect(link).toHaveAttribute("href", "/settings");
    for (const gone of ["Household settings", "Your settings", "Pockets"]) {
      expect(screen.queryByRole("link", { name: gone })).not.toBeInTheDocument();
    }
    expect(screen.getByRole("link", { name: "Budget" })).toHaveAttribute("href", "/budget");

    await user.click(link);
    expect(await screen.findByRole("heading", { level: 1, name: "Settings" })).toBeVisible();
  });
});

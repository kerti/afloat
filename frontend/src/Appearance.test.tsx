import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { toast } from "sonner";
import { alice, defaultHandlers, envelope, server, type State } from "@/test/server";
import { renderApp } from "@/test/render";
import { meKey } from "@/api/queries";

vi.mock("sonner", async () => (await import("@/test/sonnerMock")).sonnerMock());
const toastMock = vi.mocked(toast);
beforeEach(() => {
  vi.clearAllMocks();
});

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((r) => (resolve = r));
  return { promise, resolve };
}

const signedIn = (extra: Partial<State> = {}): State => ({ signedIn: true, me: alice, ...extra });
const html = () => document.documentElement;
const cache = () => window.localStorage.getItem("afloat.appearance");

async function choose(user: ReturnType<typeof userEvent.setup>, name: string) {
  const profile = within(await screen.findByRole("region", { name: /Profile|Profil/ }));
  await user.click(profile.getByRole("combobox", { name: /Appearance|Tampilan/ }));
  await user.click(await screen.findByRole("option", { name }));
}

describe("appearance picker", () => {
  it("shows the stored value, with Match this device for system", async () => {
    server.use(...defaultHandlers(signedIn()));
    renderApp("/settings");
    expect(await screen.findByRole("combobox", { name: "Appearance" })).toHaveTextContent(
      "Match this device",
    );
  });

  it("sends only {appearance}, applies at once, caches, and toasts Saved.", async () => {
    const state = signedIn();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/settings");

    await choose(user, "Dark");

    expect(html()).toHaveAttribute("data-appearance", "dark");
    expect(cache()).toBe("dark");
    await waitFor(() =>
      expect(toastMock).toHaveBeenCalledWith("Saved.", expect.objectContaining({ duration: 4000 })),
    );
    expect(state.mePatches).toEqual([{ appearance: "dark" }]);
    expect(screen.getByRole("combobox", { name: "Appearance" })).toHaveTextContent("Dark");
  });

  it("applies the choice before the server has answered", async () => {
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    server.use(
      http.patch("/api/me", async () => {
        await gate;
        return HttpResponse.json({ ...alice, appearance: "light" });
      }),
      ...defaultHandlers(signedIn()),
    );
    const user = userEvent.setup();
    renderApp("/settings");

    await choose(user, "Light");
    expect(html()).toHaveAttribute("data-appearance", "light");
    expect(cache()).toBe("light");
    release();
    await waitFor(() => expect(toastMock).toHaveBeenCalled());
  });

  it("rolls the value, the attribute and the cache back when the save fails", async () => {
    const gate = deferred();
    server.use(
      http.patch("/api/me", async () => {
        await gate.promise;
        return HttpResponse.json(envelope("VALIDATION", { field: "appearance", rule: "enum" }), {
          status: 400,
        });
      }),
      ...defaultHandlers(signedIn({ me: { ...alice, appearance: "light" } })),
    );
    const user = userEvent.setup();
    renderApp("/settings");
    await screen.findByRole("combobox", { name: "Appearance" });
    await waitFor(() => expect(cache()).toBe("light"));

    await choose(user, "Dark");
    // Applied optimistically first; the failure then takes it back.
    expect(html()).toHaveAttribute("data-appearance", "dark");
    expect(cache()).toBe("dark");
    gate.resolve();

    await waitFor(() => expect(toastMock.error).toHaveBeenCalled());
    expect(html()).toHaveAttribute("data-appearance", "light");
    expect(cache()).toBe("light");
    expect(screen.getByRole("combobox", { name: "Appearance" })).toHaveTextContent("Light");
    expect(toastMock).not.toHaveBeenCalled();
  });

  it("removes the attribute for system", async () => {
    server.use(...defaultHandlers(signedIn({ me: { ...alice, appearance: "dark" } })));
    const user = userEvent.setup();
    renderApp("/settings");
    await waitFor(() => expect(html()).toHaveAttribute("data-appearance", "dark"));

    await choose(user, "Match this device");

    expect(html()).not.toHaveAttribute("data-appearance");
    expect(cache()).toBe("system");
  });

  it("renders in Indonesian and sends the same wire value", async () => {
    const state = signedIn({ me: { ...alice, locale: "id-ID" } });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/settings");

    expect(await screen.findByRole("combobox", { name: "Tampilan" })).toHaveTextContent(
      "Ikuti perangkat ini",
    );
    await choose(user, "Gelap");
    expect(state.mePatches).toEqual([{ appearance: "dark" }]);
    await user.click(screen.getByRole("combobox", { name: "Tampilan" }));
    expect(await screen.findByRole("option", { name: "Terang" })).toBeInTheDocument();
  });
});

// Two writes in flight at once: each PATCH waits for its own gate, and the
// server's /me carries whatever the last released write set.
function twoWrites(state: State, outcomes: ["ok" | "fail", "ok" | "fail"]) {
  const gates = [deferred(), deferred()];
  let n = 0;
  server.use(
    http.patch("/api/me", async ({ request }) => {
      const i = n++;
      const body = (await request.json()) as { appearance: "light" | "dark" };
      await gates[i]!.promise;
      if (outcomes[i] === "fail") {
        return HttpResponse.json(envelope("VALIDATION", { field: "appearance", rule: "enum" }), {
          status: 400,
        });
      }
      state.me = { ...state.me, ...body };
      return HttpResponse.json(state.me);
    }),
    ...defaultHandlers(state),
  );
  return (i: number) => gates[i]!.resolve();
}

describe("appearance writes in flight", () => {
  it("leaves the newer choice alone when an earlier one fails late", async () => {
    const state = signedIn({ me: { ...alice, appearance: "system" } });
    const release = twoWrites(state, ["fail", "ok"]);
    const user = userEvent.setup();
    renderApp("/settings");
    await screen.findByRole("combobox", { name: "Appearance" });

    await choose(user, "Dark");
    await choose(user, "Light");
    expect(html()).toHaveAttribute("data-appearance", "light");

    release(0);
    await waitFor(() => expect(toastMock.error).not.toHaveBeenCalled());
    await new Promise((r) => setTimeout(r, 50));
    expect(html()).toHaveAttribute("data-appearance", "light");
    expect(cache()).toBe("light");
    expect(screen.getByRole("combobox", { name: "Appearance" })).toHaveTextContent("Light");

    release(1);
    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    expect(html()).toHaveAttribute("data-appearance", "light");
  });

  it("is not flipped by an earlier choice's late success", async () => {
    const state = signedIn();
    const release = twoWrites(state, ["ok", "ok"]);
    const user = userEvent.setup();
    renderApp("/settings");
    await screen.findByRole("combobox", { name: "Appearance" });

    await choose(user, "Dark");
    await choose(user, "Light");

    release(0);
    await new Promise((r) => setTimeout(r, 50));
    expect(html()).toHaveAttribute("data-appearance", "light");
    expect(screen.getByRole("combobox", { name: "Appearance" })).toHaveTextContent("Light");

    state.me = { ...state.me, appearance: "light" };
    release(1);
    await waitFor(() => expect(toastMock).toHaveBeenCalled());
    await waitFor(() => expect(html()).toHaveAttribute("data-appearance", "light"));
    expect(cache()).toBe("light");
  });

  it("rolls the latest choice back to the server's value when it fails", async () => {
    const state = signedIn({ me: { ...alice, appearance: "dark" } });
    const release = twoWrites(state, ["ok", "fail"]);
    const user = userEvent.setup();
    renderApp("/settings");
    await waitFor(() => expect(html()).toHaveAttribute("data-appearance", "dark"));

    await choose(user, "Light");
    await choose(user, "Match this device");
    release(0);
    release(1);

    await waitFor(() => expect(toastMock.error).toHaveBeenCalled());
    await waitFor(() => expect(html()).toHaveAttribute("data-appearance", "light"));
  });
});

describe("appearance cache", () => {
  it("lets the server's value override a stale cache once /me loads", async () => {
    window.localStorage.setItem("afloat.appearance", "dark");
    document.documentElement.setAttribute("data-appearance", "dark");
    server.use(...defaultHandlers(signedIn({ me: { ...alice, appearance: "light" } })));
    renderApp("/");

    await waitFor(() => expect(html()).toHaveAttribute("data-appearance", "light"));
    expect(cache()).toBe("light");
  });

  it("follows the cache on signed-out pages, and keeps it through sign-out", async () => {
    window.localStorage.setItem("afloat.appearance", "dark");
    server.use(...defaultHandlers(signedIn({ me: { ...alice, appearance: "dark" } })));
    const user = userEvent.setup();
    const view = renderApp("/");
    await waitFor(() => expect(html()).toHaveAttribute("data-appearance", "dark"));

    await user.click(await screen.findByRole("link", { name: "Settings" }));
    await user.click(await screen.findByRole("button", { name: "Sign out" }));
    await screen.findByRole("button", { name: /Sign in/ });

    expect(view.client.getQueryData(meKey)).toBeNull();
    expect(cache()).toBe("dark");
    expect(html()).toHaveAttribute("data-appearance", "dark");
  });

  it("gives a second User their own value after sign-in", async () => {
    window.localStorage.setItem("afloat.appearance", "dark");
    document.documentElement.setAttribute("data-appearance", "dark");
    const bob = {
      ...alice,
      email: "bob@example.com",
      user_display_name: "Bob",
      appearance: "light" as const,
    };
    const state: State = { signedIn: false, me: bob };
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/login");

    expect(html()).toHaveAttribute("data-appearance", "dark");
    await user.type(await screen.findByLabelText("Email"), "bob@example.com");
    await user.type(screen.getByLabelText("Password"), "correct-horse");
    await user.click(screen.getByRole("button", { name: "Sign in" }));

    await waitFor(() => expect(html()).toHaveAttribute("data-appearance", "light"));
    expect(cache()).toBe("light");
  });
});

describe("appearance engine missing", () => {
  it("says so once in the console, and still renders", async () => {
    const engine = window.afloatAppearance;
    delete window.afloatAppearance;
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    server.use(...defaultHandlers(signedIn()));
    try {
      vi.resetModules();
      const { renderApp: render } = await import("@/test/render");
      render("/settings");
      expect(await screen.findByRole("combobox", { name: "Appearance" })).toBeInTheDocument();
      const ours = spy.mock.calls.filter(([m]) => String(m).includes("appearance-init.js"));
      expect(ours).toHaveLength(1);
    } finally {
      spy.mockRestore();
      window.afloatAppearance = engine;
    }
  });
});

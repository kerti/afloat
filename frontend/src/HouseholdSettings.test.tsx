import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { toast } from "sonner";
import { alice, defaultHandlers, envelope, server, type State } from "@/test/server";
import currencyFixture from "../../contract/testdata/currency.json";
import { renderApp } from "@/test/render";
import { searchOptions, searchPick, selectPick } from "@/test/pickers";
import enGB from "@/locales/en-GB.json";

vi.mock("sonner", async () => (await import("@/test/sonnerMock")).sonnerMock());
const toastMock = vi.mocked(toast);
beforeEach(() => {
  vi.clearAllMocks();
});

const signedIn = (extra: Partial<State> = {}): State => ({ signedIn: true, me: alice, ...extra });

// The Household section of /settings: its Save is not the Pocket or profile one.
const household = () => within(screen.getByRole("region", { name: "Household" }));

describe("household settings", () => {
  it("is reached from the shell and shows the current values", async () => {
    server.use(...defaultHandlers(signedIn()));
    const user = userEvent.setup();
    renderApp("/");

    await user.click(await screen.findByRole("link", { name: "Settings" }));

    expect(await screen.findByLabelText("Household name")).toHaveValue("Test Household");
    expect(screen.getByRole("button", { name: "Reporting currency" })).toHaveTextContent(/^IDR/);
    expect(screen.getByRole("combobox", { name: "Budget Period starts on day" })).toHaveTextContent(
      /^1$/,
    );
    expect(screen.getByText(/Each Budget Period starts on this day/)).toBeInTheDocument();
    expect(household().getByRole("button", { name: "Save" })).toBeDisabled();
  });

  it("offers exactly the pinned currency codes, plus the current one if it is missing", async () => {
    const odd = { ...alice, household: { ...alice.household, reporting_currency: "ZWL" } };
    server.use(...defaultHandlers(signedIn({ me: odd })));
    renderApp("/settings");

    const select = await screen.findByRole("button", { name: "Reporting currency" });
    expect(select).toHaveTextContent(/^ZWL/);
    const offered = (await searchOptions(userEvent.setup(), select)).map(
      (label) => label.split(" · ")[0],
    );
    expect(offered.sort()).toEqual([...currencyFixture.codes, "ZWL"].sort());
    expect(currencyFixture.codes).not.toContain("ZWL");
  });

  it("sends only the name when only the name changed, raises one toast, and the shell shows it after", async () => {
    const state = signedIn();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/settings");

    const field = await screen.findByLabelText("Household name");
    await user.clear(field);
    await user.type(field, "Kos Kuning");
    await user.click(household().getByRole("button", { name: "Save" }));

    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    expect(toastMock).toHaveBeenCalledWith("Saved.", expect.objectContaining({ duration: 4000 }));
    expect(household().queryByRole("status")).not.toBeInTheDocument();
    expect(state.patches).toEqual([{ household_display_name: "Kos Kuning" }]);
    expect(household().getByRole("button", { name: "Save" })).toBeDisabled();

    // /me was invalidated, so the shell answers with the new name.
    await user.click(within(screen.getByRole("navigation")).getByRole("link", { name: "Home" }));
    expect(await screen.findByText("Household: Kos Kuning")).toBeInTheDocument();
  });

  it("saves on Enter, reverts on Esc, and ignores a trailing space", async () => {
    const state = signedIn();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/settings");

    const field = await screen.findByLabelText("Household name");
    await user.type(field, " ");
    expect(household().getByRole("button", { name: "Save" })).toBeDisabled();

    await user.type(field, "x");
    expect(household().getByRole("button", { name: "Save" })).toBeEnabled();
    await user.keyboard("{Escape}");
    expect(field).toHaveValue("Test Household");
    expect(household().getByRole("button", { name: "Save" })).toBeDisabled();

    await user.type(field, "!{Enter}");
    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    expect(state.patches).toEqual([{ household_display_name: "Test Household!" }]);
  });

  it("saves a chosen currency and day on change, one toast each, and nothing else", async () => {
    const state = signedIn();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/settings");

    await searchPick(
      user,
      await screen.findByRole("button", { name: "Reporting currency" }),
      "USD",
      "USD",
    );
    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    await selectPick(
      user,
      screen.getByRole("combobox", { name: "Budget Period starts on day" }),
      "25",
    );
    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(2));

    expect(toastMock).toHaveBeenNthCalledWith(
      1,
      "Saved.",
      expect.objectContaining({ duration: 4000 }),
    );
    expect(state.patches).toEqual([{ reporting_currency: "USD" }, { period_start_day: 25 }]);
    expect(screen.getByRole("button", { name: "Reporting currency" })).toHaveTextContent(/^USD/);
    expect(screen.getByRole("combobox", { name: "Budget Period starts on day" })).toHaveTextContent(
      /^25$/,
    );
    // No Save button for a choice: the only one is the name's.
    expect(household().getAllByRole("button", { name: "Save" })).toHaveLength(1);
  });

  it("shows a VALIDATION on the name inline, in words, and marks the field", async () => {
    server.use(
      http.patch("/api/household", () =>
        HttpResponse.json(
          envelope("VALIDATION", { field: "household_display_name", rule: "required" }),
          { status: 400 },
        ),
      ),
      ...defaultHandlers(signedIn()),
    );
    const user = userEvent.setup();
    renderApp("/settings");

    const field = await screen.findByLabelText("Household name");
    await user.type(field, "x");
    await user.click(household().getByRole("button", { name: "Save" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Check the Household name field.");
    expect(field).toHaveAttribute("aria-invalid", "true");
    expect(toastMock).not.toHaveBeenCalled();
    // Typing clears the server's answer.
    await user.type(field, "y");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it.each([
    [
      "reporting_currency",
      "Check the reporting currency field.",
      "Reporting currency",
      "IDR",
      "USD",
    ],
    [
      "period_start_day",
      "Check the Budget Period start day field.",
      "Budget Period starts on day",
      "1",
      "25",
    ],
  ])(
    "rolls a failed %s back and says why in an error toast",
    async (field, text, label, before, after) => {
      server.use(
        http.patch("/api/household", () =>
          HttpResponse.json(envelope("VALIDATION", { field, rule: "required" }), { status: 400 }),
        ),
        ...defaultHandlers(signedIn()),
      );
      const user = userEvent.setup();
      renderApp("/settings");

      if (field === "reporting_currency") {
        await searchPick(user, await screen.findByRole("button", { name: label }), after, after);
      } else {
        await selectPick(user, await screen.findByRole("combobox", { name: label }), after);
      }

      await waitFor(() => expect(toastMock.error).toHaveBeenCalledWith(text, expect.anything()));
      const control =
        field === "reporting_currency"
          ? screen.getByRole("button", { name: label })
          : screen.getByRole("combobox", { name: label });
      expect(control).toHaveTextContent(new RegExp(`^${before}`));
      expect(toastMock).not.toHaveBeenCalled();
    },
  );

  it("rolls a refused currency change (CURRENCY_LOCKED) back with its words in an error toast", async () => {
    server.use(
      http.patch("/api/household", () =>
        HttpResponse.json(envelope("CURRENCY_LOCKED"), { status: 409 }),
      ),
      ...defaultHandlers(signedIn()),
    );
    const user = userEvent.setup();
    renderApp("/settings");

    await searchPick(
      user,
      await screen.findByRole("button", { name: "Reporting currency" }),
      "USD",
      "USD",
    );

    await waitFor(() =>
      expect(toastMock.error).toHaveBeenCalledWith(enGB.errors.CURRENCY_LOCKED, expect.anything()),
    );
    expect(screen.getByRole("button", { name: "Reporting currency" })).toHaveTextContent(/^IDR/);
    expect(toastMock).not.toHaveBeenCalled();
  });

  it("sends the person to sign in when the session has ended", async () => {
    server.use(
      http.patch("/api/household", () =>
        HttpResponse.json(envelope("UNAUTHORIZED"), { status: 401 }),
      ),
      ...defaultHandlers(signedIn()),
    );
    const user = userEvent.setup();
    const { client } = renderApp("/settings");

    await user.type(await screen.findByLabelText("Household name"), "x");
    await user.click(household().getByRole("button", { name: "Save" }));

    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
    expect(client.getQueryData(["me"])).toBeNull();
  });

  it("shows a network failure instead of swallowing it", async () => {
    server.use(
      http.patch("/api/household", () => HttpResponse.error()),
      ...defaultHandlers(signedIn()),
    );
    const user = userEvent.setup();
    renderApp("/settings");

    await user.type(await screen.findByLabelText("Household name"), "x");
    await user.click(household().getByRole("button", { name: "Save" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Can't reach Afloat.");
    expect(toastMock).not.toHaveBeenCalled();
  });

  it("renders in Indonesian for an Indonesian User", async () => {
    server.use(...defaultHandlers(signedIn({ me: { ...alice, locale: "id-ID" } })));
    renderApp("/settings");

    expect(await screen.findByLabelText("Nama rumah tangga")).toBeInTheDocument();
    expect(screen.getByLabelText("Mata uang pelaporan")).toBeInTheDocument();
    expect(screen.getByLabelText("Periode anggaran dimulai pada tanggal")).toBeInTheDocument();
    expect(
      within(screen.getByRole("region", { name: "Rumah tangga" })).getByRole("button", {
        name: "Simpan",
      }),
    ).toBeInTheDocument();
  });

  it("sends a signed-out visitor to sign in", async () => {
    server.use(...defaultHandlers());
    renderApp("/settings");
    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
  });
});

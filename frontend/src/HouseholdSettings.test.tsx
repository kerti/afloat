import { describe, expect, it } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { alice, defaultHandlers, envelope, server, type State } from "@/test/server";
import currencyFixture from "../../contract/testdata/currency.json";
import { renderApp } from "@/test/render";

const signedIn = (extra: Partial<State> = {}): State => ({ signedIn: true, me: alice, ...extra });

describe("household settings", () => {
  it("is reached from the shell and shows the current values", async () => {
    server.use(...defaultHandlers(signedIn()));
    const user = userEvent.setup();
    renderApp("/");

    await user.click(await screen.findByRole("link", { name: "Household settings" }));

    expect(await screen.findByLabelText("Household name")).toHaveValue("Test Household");
    expect(screen.getByLabelText("Reporting currency")).toHaveValue("IDR");
    expect(screen.getByLabelText("Budget Period starts on day")).toHaveValue("1");
    expect(screen.getByText(/Each Budget Period starts on this day/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  });

  it("offers exactly the pinned currency codes, plus the current one if it is missing", async () => {
    const odd = { ...alice, household: { ...alice.household, reporting_currency: "ZWL" } };
    server.use(...defaultHandlers(signedIn({ me: odd })));
    renderApp("/settings/household");

    const select = await screen.findByLabelText("Reporting currency");
    expect(select).toHaveValue("ZWL");
    const offered = within(select)
      .getAllByRole("option")
      .map((o) => o.getAttribute("value"));
    expect(offered.sort()).toEqual([...currencyFixture.codes, "ZWL"].sort());
    expect(currencyFixture.codes).not.toContain("ZWL");
  });

  it("sends only the name when only the name changed, and the shell shows it after", async () => {
    const state = signedIn();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/settings/household");

    const field = await screen.findByLabelText("Household name");
    await user.clear(field);
    await user.type(field, "Kos Kuning");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText("Saved.")).toBeInTheDocument();
    expect(state.patches).toEqual([{ household_display_name: "Kos Kuning" }]);
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();

    // /me was invalidated, so the shell answers with the new name.
    await user.click(screen.getByRole("link", { name: "Back" }));
    expect(await screen.findByText("Household: Kos Kuning")).toBeInTheDocument();
  });

  it("sends the chosen currency and day as sent, and nothing else", async () => {
    const state = signedIn();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/settings/household");

    await user.selectOptions(await screen.findByLabelText("Reporting currency"), "USD");
    await user.selectOptions(screen.getByLabelText("Budget Period starts on day"), "25");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText("Saved.")).toBeInTheDocument();
    expect(state.patches).toEqual([{ reporting_currency: "USD", period_start_day: 25 }]);
    expect(screen.getByLabelText("Reporting currency")).toHaveValue("USD");
  });

  it.each([
    ["household_display_name", "Check the household name field.", "Household name"],
    ["reporting_currency", "Check the reporting currency field.", "Reporting currency"],
    ["period_start_day", "Check the Budget Period start day field.", "Budget Period starts on day"],
  ])("shows a VALIDATION on %s in words and marks the field", async (field, text, label) => {
    server.use(
      http.patch("/api/household", () =>
        HttpResponse.json(envelope("VALIDATION", { field, rule: "required" }), { status: 400 }),
      ),
      ...defaultHandlers(signedIn()),
    );
    const user = userEvent.setup();
    renderApp("/settings/household");

    await user.type(await screen.findByLabelText("Household name"), "x");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(text);
    expect(screen.getByLabelText(label)).toHaveAttribute("aria-invalid", "true");
    expect(screen.queryByText("Saved.")).not.toBeInTheDocument();
  });

  it("sends the person to sign in when the session has ended", async () => {
    server.use(
      http.patch("/api/household", () =>
        HttpResponse.json(envelope("UNAUTHORIZED"), { status: 401 }),
      ),
      ...defaultHandlers(signedIn()),
    );
    const user = userEvent.setup();
    const { client } = renderApp("/settings/household");

    await user.type(await screen.findByLabelText("Household name"), "x");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
    expect(client.getQueryData(["me"])).toBeNull();
  });

  it("shows a network failure instead of swallowing it", async () => {
    server.use(
      http.patch("/api/household", () => HttpResponse.error()),
      ...defaultHandlers(signedIn()),
    );
    const user = userEvent.setup();
    renderApp("/settings/household");

    await user.type(await screen.findByLabelText("Household name"), "x");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Can't reach Afloat.");
    expect(screen.queryByText("Saved.")).not.toBeInTheDocument();
  });

  it("renders in Indonesian for an Indonesian User", async () => {
    server.use(...defaultHandlers(signedIn({ me: { ...alice, locale: "id-ID" } })));
    renderApp("/settings/household");

    expect(await screen.findByLabelText("Nama rumah tangga")).toBeInTheDocument();
    expect(screen.getByLabelText("Mata uang pelaporan")).toBeInTheDocument();
    expect(screen.getByLabelText("Periode anggaran dimulai pada tanggal")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Simpan" })).toBeInTheDocument();
  });

  it("sends a signed-out visitor to sign in", async () => {
    server.use(...defaultHandlers());
    renderApp("/settings/household");
    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
  });
});

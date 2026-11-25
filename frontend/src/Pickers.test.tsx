import { describe, expect, it } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { alice, defaultHandlers, envelope, server, type State } from "@/test/server";
import { renderApp } from "@/test/render";
import enGB from "@/locales/en-GB.json";
import idID from "@/locales/id-ID.json";

const signedIn = (): State => ({
  signedIn: true,
  me: alice,
  pockets: [{ id: "p-cash", pocket_display_name: "Cash", active: true }],
  categories: [],
  defaultPocketId: "p-cash",
});

async function openCalendar(user: ReturnType<typeof userEvent.setup>) {
  server.use(...defaultHandlers(signedIn()));
  renderApp("/capture");
  await user.click(await screen.findByRole("button", { name: "Date, Today" }));
  return screen.findByRole("dialog", { name: enGB.fields.date });
}

describe("DateField's calendar", () => {
  it("names the trigger by its value and the popover by the field", async () => {
    const user = userEvent.setup();
    const calendar = await openCalendar(user);
    expect(calendar).toBeInTheDocument();
    await user.click(await within(calendar).findByRole("button", { name: /20(th)? November/ }));
    expect(screen.getByRole("button", { name: "Date, 20 November 2026" })).toBeInTheDocument();
  });

  it("heads the month as one label with month and year arrows and no native select", async () => {
    const user = userEvent.setup();
    const calendar = await openCalendar(user);
    expect(calendar.querySelector("select")).toBeNull();
    const label = within(calendar).getByText("November 2026");

    await user.click(within(calendar).getByRole("button", { name: enGB.dates.nextMonth }));
    expect(label).toHaveTextContent("December 2026");
    await user.click(within(calendar).getByRole("button", { name: enGB.dates.previousYear }));
    expect(label).toHaveTextContent("December 2025");
    await user.click(within(calendar).getByRole("button", { name: enGB.dates.nextYear }));
    await user.click(within(calendar).getByRole("button", { name: enGB.dates.previousMonth }));
    expect(label).toHaveTextContent("November 2026");
  });

  it("is worded in Indonesian", async () => {
    const user = userEvent.setup();
    server.use(...defaultHandlers({ ...signedIn(), me: { ...alice, locale: "id-ID" } }));
    renderApp("/capture");
    await user.click(
      await screen.findByRole("button", { name: `${idID.fields.date}, ${idID.dates.today}` }),
    );
    const calendar = await screen.findByRole("dialog", { name: idID.fields.date });
    expect(within(calendar).getByText("November 2026")).toBeInTheDocument();
    expect(within(calendar).getByRole("button", { name: idID.dates.nextYear })).toBeInTheDocument();
  });
});

describe("a field's error", () => {
  it("marks the Date trigger invalid and links it to the message", async () => {
    const user = userEvent.setup();
    server.use(
      http.post("/api/expenses", () =>
        HttpResponse.json(envelope("VALIDATION", { field: "occurred_on", rule: "invalid" }), {
          status: 400,
        }),
      ),
      ...defaultHandlers(signedIn()),
    );
    renderApp("/capture");
    await user.type(await screen.findByRole("textbox", { name: enGB.fields.amount }), "100");
    await user.click(screen.getByRole("button", { name: enGB.capture.add }));

    const message = await screen.findByText(enGB.capture.dateInvalid);
    const trigger = screen.getByRole("button", { name: "Date, Today" });
    expect(trigger).toHaveAttribute("aria-invalid", "true");
    expect(trigger).toHaveAttribute("aria-describedby", message.id);
  });
});

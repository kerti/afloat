import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { ExpenseRow, FilterRow } from "@/components/ExpenseList";
import { ListRow } from "@/components/List";

// PROPOSAL (polish 16): a row that goes to another screen ends in the one
// RowChevron; a row that opens a dialog, however it looks, carries none.
const props = { label: "Edit Lunch", title: "Lunch", detail: "Cash", amount: "Rp1" };

describe("RowChevron", () => {
  it("ends a row that navigates, and a filter link", () => {
    const { container } = render(
      <MemoryRouter>
        <ExpenseRow {...props} to="/expenses/1" />
        <FilterRow to="/expenses?uncategorised=true">Needs a category (1)</FilterRow>
      </MemoryRouter>,
    );
    expect(container.querySelectorAll("a svg[aria-hidden=true]")).toHaveLength(2);
  });

  it("is absent from rows that open a dialog", () => {
    const { container } = render(
      <MemoryRouter>
        <ExpenseRow {...props} onOpen={() => {}} />
        <ul>
          <ListRow label="Edit OVO" onClick={() => {}}>
            OVO
          </ListRow>
        </ul>
      </MemoryRouter>,
    );
    expect(container.querySelectorAll("svg")).toHaveLength(0);
  });
});

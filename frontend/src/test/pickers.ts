import { screen, within } from "@testing-library/react";
import type userEvent from "@testing-library/user-event";

type User = ReturnType<typeof userEvent.setup>;

// A SearchSelect (components/ui/search-select.tsx): a button that opens a popover
// with a search box and a listbox.
export async function openSearchSelect(user: User, trigger: HTMLElement) {
  await user.click(trigger);
  return screen.findByRole("combobox", { name: /^Search/ });
}

/** Opens a SearchSelect, narrows it with `query`, and picks the first option starting with `option`. */
export async function searchPick(user: User, trigger: HTMLElement, query: string, option: string) {
  const box = await openSearchSelect(user, trigger);
  await user.type(box, query);
  const choices = within(screen.getByRole("listbox")).getAllByRole("option");
  await user.click(choices.find((o) => o.textContent!.startsWith(option))!);
}

/** The option labels a SearchSelect offers, all of them, then closed again. */
export async function searchOptions(user: User, trigger: HTMLElement): Promise<string[]> {
  await openSearchSelect(user, trigger);
  const labels = within(screen.getByRole("listbox"))
    .getAllByRole("option")
    .map((o) => o.textContent!);
  await user.keyboard("{Escape}");
  return labels;
}

/** A Radix Select: open it, then pick the option. */
export async function selectPick(user: User, trigger: HTMLElement, name: string | RegExp) {
  await user.click(trigger);
  await user.click(await screen.findByRole("option", { name }));
}

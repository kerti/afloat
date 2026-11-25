import * as React from "react";
import { CaretDown } from "@phosphor-icons/react";
import { Popover } from "radix-ui";
import { pickerTrigger } from "@/components/pickerStyles";
import { cn } from "@/lib/utils";

// A long list with a search box: currency, time zone.
// No precedent in Uruni or Balances. Built from the parts Afloat already has:
// the Radix popover, the SelectTrigger's look, the SelectItem's rows. The list
// is a listbox driven from the search field (aria-activedescendant), so arrow
// keys, Enter and Esc work without leaving the field.

export type SearchOption = { value: string; label: string };

const fold = (s: string) =>
  s
    .normalize("NFD")
    .replace(/\p{Diacritic}/gu, "")
    .toLowerCase();

export function SearchSelect({
  id,
  value,
  onValueChange,
  options,
  searchLabel,
  noMatch,
  "aria-describedby": describedBy,
  "aria-invalid": invalid,
}: {
  id: string;
  value: string;
  onValueChange: (value: string) => void;
  options: SearchOption[];
  searchLabel: string;
  noMatch: string;
  "aria-describedby"?: string;
  "aria-invalid"?: boolean;
}) {
  const [open, setOpen] = React.useState(false);
  const [query, setQuery] = React.useState("");
  const [active, setActive] = React.useState(0);
  const listId = `${id}-list`;
  const listRef = React.useRef<HTMLUListElement>(null);

  const q = fold(query.trim());
  const shown = q ? options.filter((o) => fold(`${o.label} ${o.value}`).includes(q)) : options;
  const current = options.find((o) => o.value === value);

  function openChange(next: boolean) {
    setOpen(next);
    if (next) {
      setQuery("");
      setActive(
        Math.max(
          0,
          options.findIndex((o) => o.value === value),
        ),
      );
    }
  }

  React.useEffect(() => {
    if (!open) return;
    listRef.current
      ?.querySelector(`[data-index="${active}"]`)
      ?.scrollIntoView({ block: "nearest" });
  }, [active, open]);

  function choose(o: SearchOption | undefined) {
    if (!o) return;
    onValueChange(o.value);
    setOpen(false);
  }

  return (
    <Popover.Root open={open} onOpenChange={openChange}>
      <Popover.Trigger
        id={id}
        aria-describedby={describedBy}
        aria-invalid={invalid}
        className={cn(pickerTrigger, "bg-background")}
      >
        <span className="truncate">{current?.label ?? value}</span>
        <CaretDown
          aria-hidden="true"
          weight="regular"
          className="size-4 shrink-0 text-muted-foreground"
        />
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          align="start"
          sideOffset={4}
          collisionPadding={16}
          aria-label={searchLabel}
          className="z-50 flex max-h-[min(24rem,var(--radix-popover-content-available-height))] w-(--radix-popover-trigger-width) min-w-56 flex-col overflow-hidden rounded-md border bg-card text-card-foreground shadow-lg"
        >
          <div className="border-b p-1">
            <input
              type="search"
              role="combobox"
              aria-label={searchLabel}
              aria-autocomplete="list"
              aria-expanded="true"
              aria-controls={listId}
              aria-activedescendant={shown[active] ? `${listId}-${active}` : undefined}
              autoComplete="off"
              placeholder={searchLabel}
              value={query}
              onChange={(e) => {
                setQuery(e.target.value);
                setActive(0);
              }}
              onKeyDown={(e) => {
                if (e.key === "ArrowDown") {
                  e.preventDefault();
                  setActive((a) => Math.min(a + 1, shown.length - 1));
                } else if (e.key === "ArrowUp") {
                  e.preventDefault();
                  setActive((a) => Math.max(a - 1, 0));
                } else if (e.key === "Enter") {
                  e.preventDefault();
                  choose(shown[active]);
                }
              }}
              className="h-11 w-full rounded-sm bg-transparent px-3 text-base outline-none placeholder:text-muted-foreground"
            />
          </div>
          <ul
            ref={listRef}
            id={listId}
            role="listbox"
            className="min-h-0 flex-1 overflow-y-auto p-1"
          >
            {shown.length === 0 ? (
              <li className="flex min-h-11 items-center px-3 text-base text-muted-foreground">
                {noMatch}
              </li>
            ) : (
              shown.map((o, i) => (
                <li
                  key={o.value}
                  id={`${listId}-${i}`}
                  data-index={i}
                  role="option"
                  aria-selected={o.value === value}
                  onPointerMove={() => setActive(i)}
                  onClick={() => choose(o)}
                  className={cn(
                    "flex min-h-11 cursor-default items-center rounded-sm px-3 text-base select-none",
                    i === active && "bg-muted",
                    o.value === value && "font-medium",
                  )}
                >
                  {o.label}
                </li>
              ))
            )}
          </ul>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}

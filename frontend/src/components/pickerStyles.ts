// The look the date, time and search pickers share: a trigger shaped like an
// Input, and the opaque card surface of the popover it opens.
export const pickerTrigger =
  "flex h-11 w-full min-w-0 items-center justify-between gap-2 rounded-md border border-input bg-transparent px-3 text-left text-base outline-none focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/50 disabled:pointer-events-none disabled:opacity-50 aria-invalid:border-attention";

export const pickerPopover = "z-50 rounded-xl border bg-card p-3 text-card-foreground shadow-lg";

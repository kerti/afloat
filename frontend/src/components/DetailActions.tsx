import type { ReactNode } from "react";
import { Button } from "@/components/ui/button";
import { fullRow } from "@/lib/fullRow";
import { cn } from "@/lib/utils";

export interface DetailAction {
  /** Unique within the list; also the React key. */
  key: string;
  label: string;
  onClick: () => void;
  disabled?: boolean;
  /** `destructive`: removes the record (Delete). Always last, in the
   * destructive text colour. Anything else is a plain outline button. */
  tone?: "destructive";
}

// The record-level actions of an edit dialog (Deactivate, Reactivate, Delete):
// two columns of outline buttons below a hairline, the destructive one last,
// spanning both columns when it is alone in its row. The dialog's decision
// (Save) is never here: it sits in DialogFooter. (Uruni web/src/components/
// DetailActions.tsx, #768; ruled 22 Nov.) The caller passes only the actions
// that apply, in reading order, destructive last.
//
// The Expense edit route is the exception to "Save is never here": it has no
// dialog footer, so `primary` (its Save) takes the last cell, beside Delete
// and not below it, where a thumb reaching for Save could land on Delete.
export function DetailActions({
  actions,
  primary,
}: {
  actions: DetailAction[];
  primary?: ReactNode;
}) {
  if (actions.length === 0 && !primary) return null;
  const count = actions.length + (primary ? 1 : 0);
  return (
    <div className="grid grid-cols-2 gap-2 border-t border-border pt-4">
      {actions.map((action, index) => (
        <Button
          key={action.key}
          type="button"
          variant="outline"
          className={cn(
            "min-w-0",
            fullRow(count, index) && "col-span-2",
            action.tone === "destructive" && "text-destructive",
          )}
          disabled={action.disabled}
          onClick={action.onClick}
        >
          <span className="truncate">{action.label}</span>
        </Button>
      ))}
      {primary}
    </div>
  );
}

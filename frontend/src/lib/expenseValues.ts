import type { Expense, UpdateExpenseRequest } from "@/api/client";
import { compareMoney, roundMoney } from "@/lib/money";

// Radix Select cannot hold "" as an item's value, so "No Category" is this.
export const NO_CATEGORY = "none";

/** What the form hands its screen on submit. `amount` is the signed wire string
 * (a refund is negated here, R4); `categoryId` is "" (untouched), NO_CATEGORY or
 * an id; `description` is trimmed; `date` is "" while untouched (R5). */
export type ExpenseValues = {
  amount: string;
  pocketId: string;
  categoryId: string;
  description: string;
  date: string;
  time: string;
};

/** What the stored Expense lacks of the values: the fields to PATCH. The date
 * counts only when there is one (CLAUDE.md rule 5). */
export function expenseDiff(stored: Expense, v: ExpenseValues): UpdateExpenseRequest {
  const wantCategory =
    v.categoryId !== "" && v.categoryId !== NO_CATEGORY ? v.categoryId : undefined;
  const diff: UpdateExpenseRequest = {};
  if (compareMoney(stored.amount, v.amount) !== 0) diff.amount = v.amount;
  if (stored.pocket_id !== v.pocketId) diff.pocket_id = v.pocketId;
  if (stored.category_id !== wantCategory) diff.category_id = wantCategory ?? null;
  if (stored.description !== (v.description || undefined)) diff.description = v.description || null;
  if (stored.occurred_time !== (v.time || undefined)) diff.occurred_time = v.time || null;
  if (v.date !== "" && stored.occurred_on !== v.date) diff.occurred_on = v.date;
  return diff;
}

/** Edit's amount counts as changed only when the person touched the amount or
 * the Refund box: an amount stored with more decimals than the field shows is
 * not re-sent by leaving it alone. */
export function amountUntouched(stored: Expense, v: ExpenseValues): boolean {
  return compareMoney(roundMoney(stored.amount, stored.currency), v.amount) === 0;
}

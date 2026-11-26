import { useState, type FormEvent, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { ApiError, CategoryList, Expense, Me, PocketList } from "@/api/client";
import { errorMessage } from "@/api/errors";
import { CheckboxRow } from "@/components/CheckboxRow";
import { DateField } from "@/components/DateField";
import { Field, Form } from "@/components/Form";
import { MoneyInput } from "@/components/MoneyInput";
import { FieldError } from "@/components/Text";
import { TimeField } from "@/components/TimeField";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger } from "@/components/ui/select";
import { clampName } from "@/lib/codePoints";
import { NO_CATEGORY, type ExpenseValues } from "@/lib/expenseValues";
import { roundMoney } from "@/lib/money";
import { useDayHeading } from "@/lib/useDayHeading";

const DESCRIPTION_MAX = 200;
type FieldKey = "amount" | "pocket" | "category" | "date" | "description";

// The Capture form, shared by Capture (create) and the edit route (edit). The
// client derives no date: an untouched date sends none and the server dates the
// Expense (R5, CLAUDE.md rule 5). A refund is the amount with a minus sign (R4);
// edit shows a stored negative as positive with the box ticked.
export function ExpenseForm({
  me,
  mode,
  expense,
  pockets,
  categories,
  pending,
  error,
  onClearError,
  onSubmit,
  recordActions,
}: {
  me: Me;
  mode: "create" | "edit";
  /** The stored Expense: edit only. The form's state starts from it. */
  expense?: Expense;
  pockets: PocketList;
  categories: CategoryList;
  pending: boolean;
  error: ApiError | null;
  onClearError: () => void;
  onSubmit: (values: ExpenseValues) => void;
  /** Edit only: the record-level acts (DetailActions), placed between the
   * fields and Save, as an edit dialog places them above its footer. */
  recordActions?: ReactNode;
}) {
  const { t } = useTranslation();
  const dayHeading = useDayHeading(me.today);
  const currency = me.household.reporting_currency;
  const editing = mode === "edit" && expense !== undefined;

  const storedAmount = editing ? roundMoney(expense.amount, expense.currency) : "";
  const initialAmount = storedAmount.startsWith("-") ? storedAmount.slice(1) : storedAmount;
  const initialRefund = storedAmount.startsWith("-");
  const initialCategory = editing ? (expense.category_id ?? NO_CATEGORY) : "";

  const [amount, setAmount] = useState(initialAmount);
  const [refund, setRefund] = useState(initialRefund);
  const [pocketChoice, setPocketChoice] = useState<string | null>(null);
  // "" is untouched; NO_CATEGORY is a deliberate "none".
  const [category, setCategory] = useState(initialCategory);
  const [description, setDescription] = useState(editing ? (expense.description ?? "") : "");
  const [date, setDate] = useState(editing ? expense.occurred_on : "");
  // Create never pre-fills the time (R5); edit shows the stored one.
  const [time, setTime] = useState(editing ? (expense.occurred_time ?? "") : "");
  const [edited, setEdited] = useState(false);
  const [amountTouched, setAmountTouched] = useState(false);
  const [submitted, setSubmitted] = useState(false);

  // The pickers hide what is inactive (R10), but an Expense's own current Pocket
  // or Category stays as the selected option, marked, so it can be left alone.
  const pocketOptions = pockets.items.filter((p) => p.active || p.id === expense?.pocket_id);
  const categoryOptions = categories.items.filter((c) => c.active || c.id === expense?.category_id);
  const activePockets = pockets.items.filter((p) => p.active);

  // R3: the Pocket of the last Expense (the server's `default_pocket_id`), else
  // the only active Pocket, else blank. A choice that has since gone inactive
  // reads as blank, not as another Pocket the person never picked.
  const preselected = editing
    ? expense.pocket_id
    : pockets.default_pocket_id !== undefined &&
        activePockets.some((p) => p.id === pockets.default_pocket_id)
      ? pockets.default_pocket_id
      : activePockets.length === 1
        ? activePockets[0]!.id
        : "";
  const pocketId =
    pocketChoice === null
      ? preselected
      : pocketOptions.some((p) => p.id === pocketChoice)
        ? pocketChoice
        : "";
  const categoryId =
    category === NO_CATEGORY || categoryOptions.some((c) => c.id === category) ? category : "";

  const inactive = (name: string, active: boolean) =>
    active ? name : t("expenses.inactiveOption", { name });

  const local: Record<"amount" | "pocket", string | null> = {
    amount: amount === "" ? t("capture.amountRequired") : null,
    pocket: pocketId === "" ? t("capture.pocketRequired") : null,
  };

  // One code-to-field map. A server code that belongs to a field shows under it.
  let serverField: FieldKey | null = null;
  let serverText: string | null = null;
  if (error?.code === "VALIDATION") {
    const field = String(error.args.field);
    const rule = String(error.args.rule);
    if (field === "amount") {
      serverField = "amount";
      serverText = rule === "nonzero" ? t("capture.amountNonzero") : errorMessage(t, error);
    } else if (field === "pocket_id") {
      serverField = "pocket";
      serverText = t("capture.pocketUnusable");
    } else if (field === "category_id") {
      serverField = "category";
      serverText = t("capture.categoryUnusable");
    } else if (field === "occurred_on") {
      serverField = "date";
      serverText = t("capture.dateInvalid");
    } else if (field === "description") {
      serverField = "description";
      serverText = errorMessage(t, error);
    }
  }
  const serverMessage = (k: FieldKey) => (serverField === k ? serverText : null);
  const amountMessage =
    serverMessage("amount") ?? (submitted || (edited && amountTouched) ? local.amount : null);
  const pocketMessage = serverMessage("pocket") ?? (submitted ? local.pocket : null);
  const categoryMessage = serverMessage("category");
  const dateMessage = serverMessage("date");
  const descriptionMessage = serverMessage("description");
  const formMessage = error && serverField === null ? errorMessage(t, error) : null;

  function submit(e: FormEvent) {
    e.preventDefault();
    if (pending) return;
    setSubmitted(true);
    if (local.amount || local.pocket) return;
    onSubmit({
      amount: refund ? `-${amount}` : amount,
      pocketId,
      categoryId,
      description: description.trim(),
      date,
      time,
    });
  }

  return (
    <Form onSubmit={submit}>
      <Field>
        <Label htmlFor="expense-amount">{t("fields.amount")}</Label>
        <MoneyInput
          id="expense-amount"
          // The one field the person is here to fill (R15).
          autoFocus={mode === "create"}
          currency={currency}
          value={amount}
          disabled={pending}
          onChange={(wire) => {
            setAmount(wire);
            setEdited(true);
            onClearError();
          }}
          onBlur={() => setAmountTouched(true)}
          aria-invalid={amountMessage ? true : undefined}
          aria-describedby={amountMessage ? "expense-amount-error" : undefined}
        />
        {amountMessage && <FieldError id="expense-amount-error">{amountMessage}</FieldError>}
        <CheckboxRow className="justify-start" checked={refund} onChange={setRefund}>
          {t("capture.refund")}
        </CheckboxRow>
      </Field>
      <Field>
        <Label htmlFor="expense-pocket">{t("fields.pocket")}</Label>
        <Select
          value={pocketId}
          disabled={pending}
          onValueChange={(v) => {
            setPocketChoice(v);
            onClearError();
          }}
        >
          <SelectTrigger
            id="expense-pocket"
            aria-invalid={pocketMessage ? true : undefined}
            aria-describedby={pocketMessage ? "expense-pocket-error" : undefined}
          />
          <SelectContent>
            {pocketOptions.map((p) => (
              <SelectItem key={p.id} value={p.id}>
                {inactive(p.pocket_display_name, p.active)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {pocketMessage && <FieldError id="expense-pocket-error">{pocketMessage}</FieldError>}
      </Field>
      <Field>
        <Label htmlFor="expense-category">{t("fields.category")}</Label>
        <Select
          value={categoryId}
          disabled={pending}
          onValueChange={(v) => {
            setCategory(v);
            onClearError();
          }}
        >
          <SelectTrigger
            id="expense-category"
            aria-invalid={categoryMessage ? true : undefined}
            aria-describedby={categoryMessage ? "expense-category-error" : undefined}
          />
          <SelectContent>
            <SelectItem value={NO_CATEGORY}>{t("capture.noCategory")}</SelectItem>
            {categoryOptions.map((c) => (
              <SelectItem key={c.id} value={c.id}>
                {inactive(c.category_display_name, c.active)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {categoryMessage && <FieldError id="expense-category-error">{categoryMessage}</FieldError>}
      </Field>
      <Field>
        <Label htmlFor="expense-description">{t("fields.description")}</Label>
        <Input
          id="expense-description"
          value={description}
          disabled={pending}
          onChange={(e) => {
            setDescription(clampName(e.target.value, DESCRIPTION_MAX));
            onClearError();
          }}
          aria-invalid={descriptionMessage ? true : undefined}
          aria-describedby={descriptionMessage ? "expense-description-error" : undefined}
        />
        {descriptionMessage && (
          <FieldError id="expense-description-error">{descriptionMessage}</FieldError>
        )}
      </Field>
      <Field>
        <Label htmlFor="expense-date">{t("fields.date")}</Label>
        <div className="flex items-center gap-2">
          <div className="min-w-0 flex-1">
            <DateField
              id="expense-date"
              value={date}
              today={me.today}
              disabled={pending}
              format={editing ? dayHeading : undefined}
              error={dateMessage ? "expense-date-error" : undefined}
              onChange={(iso) => {
                setDate(iso);
                onClearError();
              }}
            />
          </div>
          <TimeField
            value={time}
            disabled={pending}
            onChange={(hhmm) => {
              setTime(hhmm);
              onClearError();
            }}
          />
        </div>
        {dateMessage && <FieldError id="expense-date-error">{dateMessage}</FieldError>}
      </Field>
      {formMessage && <FieldError>{formMessage}</FieldError>}
      {recordActions}
      <Button type="submit" className="w-full" disabled={pending}>
        {mode === "create"
          ? pending
            ? t("capture.adding")
            : t("capture.add")
          : pending
            ? t("expenses.saving")
            : t("expenses.save")}
      </Button>
    </Form>
  );
}

import { useEffect, useRef, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import type { Expense, Me, UpdateExpenseRequest } from "@/api/client";
import {
  categoriesKey,
  pocketsKey,
  useCategories,
  useCreateExpense,
  usePockets,
  useUpdateExpense,
} from "@/api/queries";
import { errorMessage } from "@/api/errors";
import { CheckboxRow } from "@/components/CheckboxRow";
import { DateField } from "@/components/DateField";
import { Field, Form } from "@/components/Form";
import { MoneyInput } from "@/components/MoneyInput";
import { ErrorState, Loading } from "@/components/states";
import { EmptyState, FieldError, PageTitle } from "@/components/Text";
import { TextLink } from "@/components/TextLink";
import { TimeField } from "@/components/TimeField";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger } from "@/components/ui/select";
import { clampName } from "@/lib/codePoints";
import { compareMoney, formatMoney } from "@/lib/money";
import { toastPlain } from "@/lib/plainToast";
import { routes } from "@/lib/routes";
import { uuidv7 } from "@/lib/uuid";

const DESCRIPTION_MAX = 200;
// Radix Select cannot hold "" as an item's value, so "No Category" is this.
const NO_CATEGORY = "none";

type FieldKey = "amount" | "pocket" | "category" | "date" | "description";

// Capture: a full screen, never a dialog (ui-conventions, Editing surfaces). The
// client derives no date: an untouched date sends none and the server dates the
// Expense (R5, CLAUDE.md rule 5). A refund is the amount with a minus sign (R4).
export function CapturePage({ me }: { me: Me }) {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const pockets = usePockets();
  const categories = useCategories();
  const create = useCreateExpense();
  const update = useUpdateExpense();
  const currency = me.household.reporting_currency;

  const [amount, setAmount] = useState("");
  const [refund, setRefund] = useState(false);
  const [pocketChoice, setPocketChoice] = useState<string | null>(null);
  // "" is untouched; NO_CATEGORY is a deliberate "none".
  const [category, setCategory] = useState("");
  const [description, setDescription] = useState("");
  const [date, setDate] = useState("");
  const [time, setTime] = useState("");
  const [edited, setEdited] = useState(false);
  const [amountTouched, setAmountTouched] = useState(false);
  const [submitted, setSubmitted] = useState(false);
  // One id per attempt, replaced only after a success or an ID_TAKEN, so a retry
  // after a lost response meets the server's 200 replay (#121 R5).
  const [attemptId, setAttemptId] = useState(() => uuidv7());
  // The Expense a POST answered once its reconcile PATCH failed: Add retries the
  // PATCH, never a second POST.
  const [created, setCreated] = useState<Expense | null>(null);
  // A tap on another tab while Add is pending: the save still lands and toasts,
  // but must not pull the person back to Home.
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  // A failed refetch behind data already on screen keeps the screen.
  if (!pockets.data || !categories.data) {
    const failed = pockets.error ?? categories.error;
    if (!failed) return <Loading />;
    return (
      <ErrorState
        error={failed}
        onRetry={() => {
          void pockets.refetch();
          void categories.refetch();
        }}
      />
    );
  }

  const activePockets = pockets.data.items.filter((p) => p.active);
  const activeCategories = categories.data.items.filter((c) => c.active);

  if (activePockets.length === 0) {
    return (
      <>
        <PageTitle>{t("capture.title")}</PageTitle>
        <EmptyState>{t("capture.noPocket")}</EmptyState>
        <TextLink to={routes.settings}>{t("capture.noPocketLink")}</TextLink>
      </>
    );
  }

  // R3: the Pocket of the last Expense (the server's `default_pocket_id`), else
  // the only active Pocket, else blank. A choice that has since gone inactive
  // reads as blank, not as another Pocket the person never picked.
  const preselected =
    pockets.data.default_pocket_id !== undefined &&
    activePockets.some((p) => p.id === pockets.data.default_pocket_id)
      ? pockets.data.default_pocket_id
      : activePockets.length === 1
        ? activePockets[0]!.id
        : "";
  const pocketId =
    pocketChoice === null
      ? preselected
      : activePockets.some((p) => p.id === pocketChoice)
        ? pocketChoice
        : "";
  const categoryId =
    category === NO_CATEGORY || activeCategories.some((c) => c.id === category) ? category : "";

  const pending = create.isPending || update.isPending;
  const err = create.error ?? update.error;
  const local: Record<"amount" | "pocket", string | null> = {
    amount: amount === "" ? t("capture.amountRequired") : null,
    pocket: pocketId === "" ? t("capture.pocketRequired") : null,
  };

  // One code-to-field map. A server code that belongs to a field shows under it.
  let serverField: FieldKey | null = null;
  let serverText: string | null = null;
  if (err?.code === "VALIDATION") {
    const field = String(err.args.field);
    const rule = String(err.args.rule);
    if (field === "amount") {
      serverField = "amount";
      serverText = rule === "nonzero" ? t("capture.amountNonzero") : errorMessage(t, err);
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
      serverText = errorMessage(t, err);
    }
  }
  const serverMessage = (k: FieldKey) => (serverField === k ? serverText : null);
  const amountMessage =
    serverMessage("amount") ?? (submitted || (edited && amountTouched) ? local.amount : null);
  const pocketMessage = serverMessage("pocket") ?? (submitted ? local.pocket : null);
  const categoryMessage = serverMessage("category");
  const dateMessage = serverMessage("date");
  const descriptionMessage = serverMessage("description");
  const formMessage = err && serverField === null ? errorMessage(t, err) : null;

  const clearServer = () => {
    if (create.error) create.reset();
    if (update.error) update.reset();
  };

  // What the stored Expense lacks of the form: the fields to PATCH. A lost reply
  // makes the server replay the first body, so the form may have moved on since.
  // The date counts only when the person picked one (CLAUDE.md rule 5).
  function differences(stored: Expense, wire: string, note: string): UpdateExpenseRequest {
    const wantCategory = categoryId !== "" && categoryId !== NO_CATEGORY ? categoryId : undefined;
    const diff: UpdateExpenseRequest = {};
    if (compareMoney(stored.amount, wire) !== 0) diff.amount = wire;
    if (stored.pocket_id !== pocketId) diff.pocket_id = pocketId;
    if (stored.category_id !== wantCategory) diff.category_id = wantCategory ?? null;
    if (stored.description !== (note || undefined)) diff.description = note || null;
    if (stored.occurred_time !== (time || undefined)) diff.occurred_time = time || null;
    if (date !== "" && stored.occurred_on !== date) diff.occurred_on = date;
    return diff;
  }

  async function save(wire: string, note: string): Promise<Expense> {
    const stored =
      created ??
      (await create.mutateAsync({
        id: attemptId,
        amount: wire,
        pocket_id: pocketId,
        ...(categoryId !== "" && categoryId !== NO_CATEGORY && { category_id: categoryId }),
        ...(date !== "" && { occurred_on: date }),
        ...(time !== "" && { occurred_time: time }),
        ...(note !== "" && { description: note }),
      }));
    const diff = differences(stored, wire, note);
    if (Object.keys(diff).length === 0) return stored;
    setCreated(stored);
    try {
      return await update.mutateAsync({ id: stored.id, body: diff });
    } catch (e) {
      // The Expense is gone since the POST: start over with a fresh id, on this
      // same tap (a render-time `created` would still be null here).
      if ((e as { code?: string }).code === "NOT_FOUND") {
        setCreated(null);
        setAttemptId(uuidv7());
      }
      throw e;
    }
  }

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (pending) return;
    setSubmitted(true);
    if (local.amount || local.pocket) return;
    const wire = refund ? `-${amount}` : amount;
    const note = description.trim();
    save(wire, note)
      .then((stored) => {
        setAttemptId(uuidv7());
        setCreated(null);
        toastPlain(
          t("toast.added", { name: formatMoney(stored.amount, stored.currency, i18n.language) }),
        );
        if (mounted.current) void navigate(routes.home);
      })
      .catch((e: unknown) => {
        const code = (e as { code?: string }).code;
        if (code === "ID_TAKEN") setAttemptId(uuidv7());
        // A Pocket or Category gone inactive or deleted: the lists are stale.
        if (code === "VALIDATION") {
          const field = (e as { args?: { field?: string } }).args?.field;
          if (field === "pocket_id") void qc.invalidateQueries({ queryKey: pocketsKey });
          if (field === "category_id") void qc.invalidateQueries({ queryKey: categoriesKey });
        }
      });
  }

  return (
    <>
      <PageTitle>{t("capture.title")}</PageTitle>
      <Form onSubmit={onSubmit}>
        <Field>
          <Label htmlFor="expense-amount">{t("fields.amount")}</Label>
          <MoneyInput
            id="expense-amount"
            // The one field the person is here to fill (R15).
            autoFocus
            currency={currency}
            value={amount}
            disabled={pending}
            onChange={(wire) => {
              setAmount(wire);
              setEdited(true);
              clearServer();
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
              clearServer();
            }}
          >
            <SelectTrigger
              id="expense-pocket"
              aria-invalid={pocketMessage ? true : undefined}
              aria-describedby={pocketMessage ? "expense-pocket-error" : undefined}
            />
            <SelectContent>
              {activePockets.map((p) => (
                <SelectItem key={p.id} value={p.id}>
                  {p.pocket_display_name}
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
              clearServer();
            }}
          >
            <SelectTrigger
              id="expense-category"
              aria-invalid={categoryMessage ? true : undefined}
              aria-describedby={categoryMessage ? "expense-category-error" : undefined}
            />
            <SelectContent>
              <SelectItem value={NO_CATEGORY}>{t("capture.noCategory")}</SelectItem>
              {activeCategories.map((c) => (
                <SelectItem key={c.id} value={c.id}>
                  {c.category_display_name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {categoryMessage && (
            <FieldError id="expense-category-error">{categoryMessage}</FieldError>
          )}
        </Field>
        <Field>
          <Label htmlFor="expense-description">{t("fields.description")}</Label>
          <Input
            id="expense-description"
            value={description}
            disabled={pending}
            onChange={(e) => {
              setDescription(clampName(e.target.value, DESCRIPTION_MAX));
              clearServer();
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
                error={dateMessage ? "expense-date-error" : undefined}
                onChange={(iso) => {
                  setDate(iso);
                  clearServer();
                }}
              />
            </div>
            <TimeField
              value={time}
              disabled={pending}
              onChange={(hhmm) => {
                setTime(hhmm);
                clearServer();
              }}
            />
          </div>
          {dateMessage && <FieldError id="expense-date-error">{dateMessage}</FieldError>}
        </Field>
        {formMessage && <FieldError>{formMessage}</FieldError>}
        <Button type="submit" className="w-full" disabled={pending}>
          {pending ? t("capture.adding") : t("capture.add")}
        </Button>
      </Form>
    </>
  );
}

import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import type { Expense, Me } from "@/api/client";
import {
  categoriesKey,
  pocketsKey,
  useCategories,
  useCreateExpense,
  usePockets,
  useUpdateExpense,
} from "@/api/queries";
import { ExpenseForm } from "@/components/ExpenseForm";
import { NO_CATEGORY, expenseDiff, type ExpenseValues } from "@/lib/expenseValues";
import { ErrorState, Loading } from "@/components/states";
import { EmptyState, PageTitle } from "@/components/Text";
import { TextLink } from "@/components/TextLink";
import { formatMoney } from "@/lib/money";
import { routes } from "@/lib/routes";
import { toastWithAction } from "@/lib/undoToast";
import { uuidv7 } from "@/lib/uuid";

// Capture: a full screen, never a dialog (ui-conventions, Editing surfaces). The
// form body is shared with the edit route (components/ExpenseForm.tsx).
export function CapturePage({ me }: { me: Me }) {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const pockets = usePockets();
  const categories = useCategories();
  const create = useCreateExpense();
  const update = useUpdateExpense();

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

  if (!pockets.data.items.some((p) => p.active)) {
    return (
      <>
        <PageTitle>{t("capture.title")}</PageTitle>
        <EmptyState>{t("capture.noPocket")}</EmptyState>
        <TextLink to={routes.settings}>{t("capture.noPocketLink")}</TextLink>
      </>
    );
  }

  async function save(values: ExpenseValues): Promise<Expense> {
    const { categoryId, pocketId, date, time, description: note } = values;
    const stored =
      created ??
      (await create.mutateAsync({
        id: attemptId,
        amount: values.amount,
        pocket_id: pocketId,
        ...(categoryId !== "" && categoryId !== NO_CATEGORY && { category_id: categoryId }),
        ...(date !== "" && { occurred_on: date }),
        ...(time !== "" && { occurred_time: time }),
        ...(note !== "" && { description: note }),
      }));
    // A lost reply makes the server replay the first body, so the form may have
    // moved on since: PATCH what the stored Expense lacks.
    const diff = expenseDiff(stored, values);
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

  function onSubmit(values: ExpenseValues) {
    save(values)
      .then((stored) => {
        setAttemptId(uuidv7());
        setCreated(null);
        // The Edit action opens the Expense just saved. The Toaster is at the
        // root, so it works after this screen has gone.
        toastWithAction(
          t("toast.added", { name: formatMoney(stored.amount, stored.currency, i18n.language) }),
          t("toast.edit"),
          () => void navigate(routes.expense(stored.id)),
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
      <ExpenseForm
        me={me}
        mode="create"
        pockets={pockets.data}
        categories={categories.data}
        pending={create.isPending || update.isPending}
        error={create.error ?? update.error}
        onClearError={() => {
          if (create.error) create.reset();
          if (update.error) update.reset();
        }}
        onSubmit={onSubmit}
      />
    </>
  );
}

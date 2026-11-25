import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { useLocation, useNavigate, useParams } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { ApiError, type Me } from "@/api/client";
import {
  expensesKey,
  useCategories,
  useExpense,
  usePockets,
  useUpdateExpense,
} from "@/api/queries";
import { errorMessage } from "@/api/errors";
import { BackLink } from "@/components/BackLink";
import { ExpenseForm } from "@/components/ExpenseForm";
import { amountUntouched, expenseDiff, type ExpenseValues } from "@/lib/expenseValues";
import { ErrorState, Loading } from "@/components/states";
import { PageTitle } from "@/components/Text";
import { toastError, toastSaved } from "@/lib/plainToast";
import { routes } from "@/lib/routes";

// An Expense is edited on a route, the Capture form in edit mode (#140 R17).
// Save sends only the fields that differ from the stored Expense, then goes back
// to where the person came from; a direct link has no in-app history behind it,
// so it lands on the list.
export function EditExpensePage({ me }: { me: Me }) {
  const { t } = useTranslation();
  const { id = "" } = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const qc = useQueryClient();
  const expense = useExpense(id);
  const pockets = usePockets();
  const categories = useCategories();
  const update = useUpdateExpense();
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  // Gone before the screen could show it (deleted, or a stale link): there is
  // nothing to retry, so it is what a Save that finds it gone does: a toast, a
  // refetch of the lists and the list. Any other failure keeps ErrorState.
  const goneError =
    !expense.data && expense.error instanceof ApiError && expense.error.code === "NOT_FOUND"
      ? expense.error
      : null;
  const gone = goneError !== null;
  const goneHandled = useRef(false);
  useEffect(() => {
    if (!goneError || goneHandled.current) return;
    goneHandled.current = true;
    toastError(errorMessage(t, goneError));
    void qc.invalidateQueries({ queryKey: expensesKey });
    void navigate(routes.expenses, { replace: true });
  }, [goneError, navigate, qc, t]);

  const head = (
    <>
      <BackLink to={routes.expenses} label={t("expenses.title")} />
      <PageTitle>{t("expenses.editTitle")}</PageTitle>
    </>
  );

  // A failed refetch behind data already on screen keeps the screen.
  if (!expense.data || !pockets.data || !categories.data) {
    const failed =
      (!expense.data && expense.error) ||
      (!pockets.data && pockets.error) ||
      (!categories.data && categories.error) ||
      null;
    return (
      <>
        {head}
        {failed && !gone ? (
          <ErrorState
            error={failed}
            onRetry={() => {
              void expense.refetch();
              void pockets.refetch();
              void categories.refetch();
            }}
          />
        ) : (
          <Loading />
        )}
      </>
    );
  }

  const stored = expense.data;

  // "Back" is history back when the person came from inside the app, else the list.
  function leave() {
    if (!mounted.current) return;
    if (location.key !== "default") void navigate(-1);
    else void navigate(routes.expenses, { replace: true });
  }

  function onSubmit(values: ExpenseValues) {
    const diff = expenseDiff(stored, values);
    if (amountUntouched(stored, values)) delete diff.amount;
    const write =
      Object.keys(diff).length === 0
        ? Promise.resolve()
        : update.mutateAsync({ id: stored.id, body: diff });
    write
      .then(() => {
        toastSaved();
        leave();
      })
      .catch((e: unknown) => {
        // Gone while editing (another member deleted it): no form is left to
        // hold the message, so it is a toast, a refetch and back to the list.
        if (e instanceof ApiError && e.code === "NOT_FOUND") {
          toastError(errorMessage(t, e));
          void qc.invalidateQueries({ queryKey: expensesKey });
          if (mounted.current) void navigate(routes.expenses, { replace: true });
        }
      });
  }

  return (
    <>
      {head}
      <ExpenseForm
        key={stored.id}
        me={me}
        mode="edit"
        expense={stored}
        pockets={pockets.data}
        categories={categories.data}
        pending={update.isPending}
        error={update.error}
        onClearError={() => {
          if (update.error) update.reset();
        }}
        onSubmit={onSubmit}
      />
    </>
  );
}

import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
import { updateCategory, type ApiError, type Category, type Me } from "@/api/client";
import {
  budgetKey,
  categoriesKey,
  useBudget,
  useCategories,
  useUpdateBudget,
  useUpdateCategory,
} from "@/api/queries";
import { errorMessage } from "@/api/errors";
import { CategoryEditDialog } from "@/components/CategoryEditDialog";
import { CheckboxRow } from "@/components/CheckboxRow";
import { BackLink } from "@/components/BackLink";
import { CategoryCreateDialog } from "@/components/CategoryCreateDialog";
import { FigureRow } from "@/components/FigureRow";
import { Section } from "@/components/Form";
import { Badge } from "@/components/Badge";
import { ListCard, ListRow } from "@/components/List";
import { MoneySetting } from "@/components/MoneySetting";
import { ErrorState, Loading } from "@/components/states";
import { EmptyState, MutedText, PageTitle } from "@/components/Text";
import { Button } from "@/components/ui/button";
import { formatDateRange } from "@/lib/dates";
import { formatMoney } from "@/lib/money";
import { toastError } from "@/lib/plainToast";
import { routes } from "@/lib/routes";
import { useDialogParam } from "@/lib/useDialogParam";
import { toastUndoable } from "@/lib/undoToast";
import { uuidv7 } from "@/lib/uuid";

// The Budget screen. Every figure is the server's answer; the client adds none
// and derives no date (CLAUDE.md rule 5, PRD N5a). A Category's allocation is
// the Budget's, joined to the Category by id.
// An allocation of zero is still an allocation; only a non-zero one keeps an
// inactive Category on screen (#130 R9). Tested on the digits, never parsed.
const EDIT_PREFIX = "category:";
const nonZero = (amount: string) => /[1-9]/.test(amount);
export function BudgetPage({ me }: { me: Me }) {
  const { t, i18n } = useTranslation();
  const budget = useBudget();
  const categories = useCategories();
  const newParam = useDialogParam("new");
  const editParam = useDialogParam("edit");
  const qc = useQueryClient();
  const toggle = useUpdateCategory();
  // Page state, not stored: unticked again on every visit (R7).
  const [showInactive, setShowInactive] = useState(false);
  // One id per create attempt, kept here so closing and reopening the dialog
  // after a failure still resends it.
  const [attemptId, setAttemptId] = useState(uuidv7);
  const update = useUpdateBudget();
  const currency = me.household.reporting_currency;
  const data = budget.data;
  const money = (v: string) => formatMoney(v, currency, i18n.language);
  const allocationOf = new Map(data?.allocations.map((a) => [a.category_id, a.allocation]));
  // An inactive Category holding a non-zero allocation is always listed (R9);
  // the others wait behind "Show inactive Categories (N)", N counting only
  // those still hidden.
  const held = (c: Category) => c.active || nonZero(allocationOf.get(c.id) ?? "0");
  const hiddenCount = categories.data?.items.filter((c) => !held(c)).length ?? 0;
  const listed = categories.data?.items.filter((c) => held(c) || showInactive);
  const editing =
    categories.data && data && editParam.value?.startsWith(EDIT_PREFIX)
      ? categories.data.items.find((c) => c.id === editParam.value!.slice(EDIT_PREFIX.length))
      : undefined;
  // A link to a Category that is not there drops the param rather than showing
  // an empty dialog.
  const staleEdit = categories.data && data && editParam.value !== null && !editing;
  const clearEdit = editParam.clear;
  useEffect(() => {
    if (staleEdit) clearEdit();
  }, [staleEdit, clearEdit]);

  // Deactivating and reactivating are immediate, with no confirmation: the dialog
  // closes at once and the Undo toast is the way back. Toasts come from
  // mutateAsync().then, never mutate's per-call onSuccess. With the dialog
  // closed, a failure is a toast too.
  function onToggle(c: Category) {
    if (toggle.isPending) return;
    const name = c.category_display_name;
    editParam.close();
    toggle
      .mutateAsync({ id: c.id, body: { active: !c.active } })
      .then(() => {
        toastUndoable({
          message: t(c.active ? "toast.deactivated" : "toast.reactivated", { name }),
          undo: () =>
            updateCategory(c.id, { active: c.active }).then((r) =>
              qc.invalidateQueries({ queryKey: budgetKey }).then(() => r),
            ),
          qc,
          invalidate: categoriesKey,
        });
      })
      .catch((e: ApiError) => {
        toastError(errorMessage(t, e));
      });
  }
  const buckets = [
    { bucket: "commitment", id: "commitments", empty: "categories.commitmentsEmpty" },
    { bucket: "daily", id: "daily", empty: "categories.dailyEmpty" },
  ] as const;

  return (
    <>
      <BackLink to={routes.home} label={t("home.title")} />
      <PageTitle>{t("budget.title")}</PageTitle>
      {budget.error ? (
        <ErrorState error={budget.error} onRetry={() => void budget.refetch()} />
      ) : !data ? (
        <Loading />
      ) : (
        <>
          <MutedText>
            {t(
              "budget.window",
              formatDateRange(data.period.starts_on, data.period.ends_on, i18n.language),
            )}
          </MutedText>
          <Section>
            <MoneySetting
              id="monthly-budget"
              label={t("fields.monthlyBudget")}
              currency={currency}
              stored={data.monthly_budget}
              save={(next) => update.mutateAsync({ monthly_budget: next })}
              pending={update.isPending}
              error={update.error}
              clearError={update.reset}
            />
          </Section>
          {data.daily_pool === undefined || data.unallocated === undefined ? (
            <EmptyState>{t("budget.empty")}</EmptyState>
          ) : (
            <ListCard>
              <FigureRow label={t("budget.commitmentsBudget")}>
                {money(data.commitments_budget)}
              </FigureRow>
              <FigureRow label={t("budget.dailyPool")}>{money(data.daily_pool)}</FigureRow>
              <FigureRow label={t("budget.unallocated")}>{money(data.unallocated)}</FigureRow>
            </ListCard>
          )}
          {data.unallocated?.startsWith("-") && <MutedText>{t("budget.overAllocated")}</MutedText>}
          {hiddenCount > 0 && (
            <CheckboxRow checked={showInactive} onChange={setShowInactive}>
              {t("categories.showInactive", { count: hiddenCount })}
            </CheckboxRow>
          )}
          {categories.error ? (
            <ErrorState error={categories.error} onRetry={() => void categories.refetch()} />
          ) : !listed ? (
            <Loading />
          ) : (
            buckets.map(({ bucket, id, empty }) => {
              const rows = listed.filter((c) => c.bucket === bucket);
              return (
                <Section key={bucket} id={id} title={t(`categories.${id}Heading`)}>
                  {rows.length === 0 ? (
                    <EmptyState>{t(empty)}</EmptyState>
                  ) : (
                    <ListCard>
                      {rows.map((c) => {
                        const amount = allocationOf.get(c.id);
                        return (
                          <ListRow
                            key={c.id}
                            label={t(
                              c.active ? "categories.editFor" : "categories.editForInactive",
                              {
                                name: c.category_display_name,
                                amount:
                                  amount === undefined
                                    ? t("categories.noAllocation")
                                    : money(amount),
                              },
                            )}
                            onClick={() => editParam.open(EDIT_PREFIX + c.id)}
                            muted={!c.active}
                            badge={c.active ? undefined : <Badge>{t("pockets.inactive")}</Badge>}
                            trailing={
                              amount === undefined ? t("categories.noAllocation") : money(amount)
                            }
                            trailingMuted={amount === undefined}
                          >
                            {c.category_display_name}
                          </ListRow>
                        );
                      })}
                    </ListCard>
                  )}
                </Section>
              );
            })
          )}
          <Button className="w-full" onClick={() => newParam.open("category")}>
            {t("categories.addHeading")}
          </Button>
        </>
      )}
      {editing && (
        <CategoryEditDialog
          key={editing.id}
          category={editing}
          stored={allocationOf.get(editing.id)}
          currency={currency}
          close={editParam.close}
          onToggle={onToggle}
        />
      )}
      {newParam.value === "category" && (
        <CategoryCreateDialog
          currency={currency}
          attemptId={attemptId}
          newAttemptId={() => setAttemptId(uuidv7())}
          close={newParam.close}
        />
      )}
    </>
  );
}

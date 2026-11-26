import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { ApiError, type Expense, type Me } from "@/api/client";
import { useCategories, useExpenses, usePockets, useRestoreExpense } from "@/api/queries";
import { errorMessage } from "@/api/errors";
import { BackLink } from "@/components/BackLink";
import { DetailActions } from "@/components/DetailActions";
import { ExpenseRow, LoadMore } from "@/components/ExpenseList";
import { FigureRow } from "@/components/FigureRow";
import { ListCard } from "@/components/List";
import { ErrorState, Loading } from "@/components/states";
import { EmptyState, FieldError, PageTitle } from "@/components/Text";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { formatMoney } from "@/lib/money";
import { toastError, toastPlain } from "@/lib/plainToast";
import { routes } from "@/lib/routes";
import { useDayHeading } from "@/lib/useDayHeading";
import { useDialogParam } from "@/lib/useDialogParam";

const VIEW_PARAM = "expense";

// The Recycle Bin: every deleted Expense, the last deleted first, a page at a
// time (#140 R11). A row opens a read-only dialog whose one act is Restore.
// The bin has no day headings: its order is by when each was deleted, so days
// would repeat.
export function RecycleBinPage({ me }: { me: Me }) {
  const { t, i18n } = useTranslation();
  const bin = useExpenses(false, true);
  const pockets = usePockets();
  const categories = useCategories();
  const heading = useDayHeading(me.today);
  const view = useDialogParam(VIEW_PARAM);
  const restore = useRestoreExpense();
  const [actionErr, setActionErr] = useState<ApiError | null>(null);

  const items = bin.data?.pages.flatMap((page) => page.items) ?? [];
  const open = view.value !== null ? items.find((e) => e.id === view.value) : undefined;

  // A link to an Expense that is not in the bin (restored meanwhile, or never
  // there) clears the parameter once the whole bin is known.
  const settled = bin.data !== undefined && !bin.hasNextPage && !bin.isFetching;
  const { clear } = view;
  useEffect(() => {
    if (view.value !== null && settled && open === undefined && !restore.isPending) clear();
  }, [view.value, settled, open, restore.isPending, clear]);

  const head = (
    <>
      <BackLink to={routes.expenses} label={t("expenses.title")} />
      <PageTitle>{t("expenses.recycleBin")}</PageTitle>
    </>
  );

  const failed =
    (!bin.data && bin.error) ||
    (!pockets.data && pockets.error) ||
    (!categories.data && categories.error) ||
    null;
  if (failed) {
    return (
      <>
        {head}
        <ErrorState
          error={failed}
          onRetry={() => {
            void bin.refetch();
            void pockets.refetch();
            void categories.refetch();
          }}
        />
      </>
    );
  }
  if (!bin.data || !pockets.data || !categories.data) {
    return (
      <>
        {head}
        <Loading />
      </>
    );
  }

  const pocketName = new Map(pockets.data.items.map((p) => [p.id, p.pocket_display_name]));
  const categoryName = new Map(categories.data.items.map((c) => [c.id, c.category_display_name]));
  const amountOf = (e: Expense) => formatMoney(e.amount, e.currency, i18n.language);
  const titleOf = (e: Expense) =>
    e.description ?? (e.category_id ? categoryName.get(e.category_id) : undefined);

  async function onRestore(e: Expense) {
    if (restore.isPending) return;
    setActionErr(null);
    try {
      await restore.mutateAsync(e.id);
    } catch (err) {
      const apiErr = err instanceof ApiError ? err : new ApiError(0, "UNKNOWN");
      if (apiErr.code === "NOT_FOUND") {
        // Gone meanwhile: no dialog is worth keeping open on it.
        toastError(errorMessage(t, apiErr));
        view.close();
      } else {
        setActionErr(apiErr);
      }
      return;
    }
    view.close();
    toastPlain(t("toast.restored", { name: amountOf(e) }));
  }

  return (
    <>
      {head}
      {items.length === 0 && <EmptyState>{t("expenses.binEmpty")}</EmptyState>}
      {items.length > 0 && (
        <ListCard>
          {items.map((e) => {
            const category = e.category_id ? categoryName.get(e.category_id) : undefined;
            const title = titleOf(e);
            const detail = [
              heading(e.occurred_on),
              pocketName.get(e.pocket_id),
              e.description !== undefined ? category : undefined,
              e.occurred_time,
            ]
              .filter((part): part is string => part !== undefined && part !== "")
              .join(" · ");
            const amount = amountOf(e);
            const name = title ?? t("expenses.uncategorised");
            return (
              <ExpenseRow
                key={e.id}
                onOpen={() => {
                  setActionErr(null);
                  view.open(e.id);
                }}
                label={t("expenses.binRow", { name, amount })}
                title={name}
                muted={title === undefined}
                detail={detail}
                amount={amount}
              />
            );
          })}
        </ListCard>
      )}
      {bin.isFetchNextPageError && bin.error ? (
        <ErrorState error={bin.error} onRetry={() => void bin.fetchNextPage()} />
      ) : (
        bin.hasNextPage && (
          <LoadMore loading={bin.isFetchingNextPage} onClick={() => void bin.fetchNextPage()} />
        )
      )}
      <Dialog
        open={open !== undefined}
        // Not dismissible while the Restore is in flight.
        onOpenChange={(next) => {
          if (!next && !restore.isPending) view.close();
        }}
      >
        {open && (
          <DialogContent closeLabel={t("dialog.close")}>
            <DialogHeader>
              <DialogTitle>{t("expenses.binDialogTitle")}</DialogTitle>
            </DialogHeader>
            <ListCard>
              <FigureRow label={t("fields.amount")}>{amountOf(open)}</FigureRow>
              <FigureRow label={t("fields.pocket")}>
                {pocketName.get(open.pocket_id) ?? ""}
              </FigureRow>
              {open.category_id && (
                <FigureRow label={t("expenses.categoryLabel")}>
                  {categoryName.get(open.category_id) ?? ""}
                </FigureRow>
              )}
              {open.description !== undefined && (
                <FigureRow label={t("expenses.descriptionLabel")}>{open.description}</FigureRow>
              )}
              <FigureRow label={t("fields.date")}>{heading(open.occurred_on)}</FigureRow>
              {open.occurred_time !== undefined && (
                <FigureRow label={t("dates.timePicker")}>{open.occurred_time}</FigureRow>
              )}
            </ListCard>
            {actionErr && <FieldError>{errorMessage(t, actionErr)}</FieldError>}
            <DetailActions
              actions={[
                {
                  key: "restore",
                  label: t("expenses.restore"),
                  disabled: restore.isPending,
                  onClick: () => void onRestore(open),
                },
              ]}
            />
          </DialogContent>
        )}
      </Dialog>
    </>
  );
}

import { useEffect, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { updatePocket, type ApiError, type Pocket } from "@/api/client";
import { pocketsKey, useCreatePocket, usePockets, useUpdatePocket } from "@/api/queries";
import { errorMessage } from "@/api/errors";
import { ErrorState, Loading } from "@/components/states";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { toastUndoable } from "@/lib/undoToast";
import { useDialogParam } from "@/lib/useDialogParam";
import { uuidv7 } from "@/lib/uuid";

// A plain toast lasts 4 s; one with an Undo lasts 8 s (lib/undoToast.ts).
const PLAIN_TOAST_MS = 4000;

const EDIT_PREFIX = "pocket:";

type PocketFormProps = {
  mode: "create" | "edit";
  // Edit only.
  pocket?: Pocket;
  // Create only: the id of this attempt, and the way to replace it.
  attemptId?: string;
  newAttemptId?: () => void;
  close: () => void;
  onToggle?: (p: Pocket) => void;
};

function PocketForm({ mode, pocket, attemptId, newAttemptId, close, onToggle }: PocketFormProps) {
  const { t } = useTranslation();
  const create = useCreatePocket();
  const update = useUpdatePocket();
  const write = mode === "create" ? create : update;
  // Initialised from the record, never reset in an effect; the caller keys the
  // form on the Pocket's id.
  const [name, setName] = useState(() => pocket?.pocket_display_name ?? "");
  const [touched, setTouched] = useState(false);
  const [submitted, setSubmitted] = useState(false);

  const pending = write.isPending;
  const unchanged = mode === "edit" && name.trim() === pocket?.pocket_display_name;
  const blank = name.trim() === "";
  const err = write.error;
  const fieldErr =
    err !== null &&
    (err.code === "NAME_TAKEN" ||
      (err.code === "VALIDATION" && err.args.field === "pocket_display_name"));
  // One message under the field: the server's code wins over the local check.
  const fieldMessage = fieldErr
    ? errorMessage(t, err)
    : (touched || submitted) && blank
      ? t("pockets.nameRequired")
      : null;
  const formMessage = err && !fieldErr ? errorMessage(t, err) : null;

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (pending || unchanged) return;
    setSubmitted(true);
    if (blank) return;
    // mutateAsync, not mutate's per-call onSuccess: TanStack drops per-call
    // callbacks once the component unmounts. A failure is shown from
    // write.error, so it is swallowed here.
    if (mode === "create") {
      create
        .mutateAsync({ id: attemptId!, pocket_display_name: name })
        .then((p) => {
          // Replaced only after a success, so a retry after a lost response
          // resends the id and meets the server's 200 replay, never a duplicate.
          newAttemptId?.();
          toast(t("toast.added", { name: p.pocket_display_name }), { duration: PLAIN_TOAST_MS });
          close();
        })
        .catch((e: unknown) => {
          // ID_TAKEN means nothing was written under this id for this caller,
          // so resending it can only fail again.
          if ((e as { code?: string }).code === "ID_TAKEN") newAttemptId?.();
        });
      return;
    }
    update
      .mutateAsync({ id: pocket!.id, body: { pocket_display_name: name } })
      .then(() => {
        toast(t("toast.saved"), { duration: PLAIN_TOAST_MS });
        close();
      })
      .catch(() => {});
  }

  return (
    <form onSubmit={onSubmit} className="grid grid-cols-1 gap-4" noValidate>
      <DialogHeader>
        <DialogTitle>
          {t(mode === "create" ? "pockets.addHeading" : "pockets.editHeading")}
        </DialogTitle>
      </DialogHeader>
      <div className="flex flex-col gap-2">
        <Label htmlFor="pocket-display-name">{t("fields.pocketDisplayName")}</Label>
        <Input
          id="pocket-display-name"
          value={name}
          maxLength={60}
          placeholder={t("pockets.namePlaceholder")}
          disabled={pending}
          onChange={(e) => {
            setName(e.target.value);
            if (err) write.reset();
          }}
          onBlur={() => setTouched(true)}
          aria-invalid={fieldMessage ? true : undefined}
          aria-describedby={fieldMessage ? "pocket-name-error" : undefined}
        />
        {fieldMessage && (
          <p id="pocket-name-error" role="alert" className="text-sm text-destructive">
            {fieldMessage}
          </p>
        )}
      </div>
      {formMessage && (
        <p role="alert" className="text-sm text-destructive">
          {formMessage}
        </p>
      )}
      <DialogFooter>
        {mode === "create" ? (
          <Button type="button" variant="outline" disabled={pending} onClick={close}>
            {t("pockets.cancel")}
          </Button>
        ) : (
          <Button
            type="button"
            variant="outline"
            disabled={pending}
            onClick={() => onToggle?.(pocket!)}
          >
            {t(pocket!.active ? "pockets.deactivate" : "pockets.reactivate")}
          </Button>
        )}
        <Button type="submit" disabled={pending || unchanged}>
          {pending
            ? t(mode === "create" ? "pockets.adding" : "pockets.saving")
            : t(mode === "create" ? "pockets.add" : "pockets.save")}
        </Button>
      </DialogFooter>
    </form>
  );
}

export function PocketsPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const pockets = usePockets();
  const toggle = useUpdatePocket();
  const editParam = useDialogParam("edit");
  const newParam = useDialogParam("new");
  // Page state, not stored: unticked again on every visit (R7).
  const [showInactive, setShowInactive] = useState(false);
  // One id per create attempt, kept here so closing and reopening the dialog
  // after a failure still resends it.
  const [attemptId, setAttemptId] = useState(uuidv7);

  // Deactivating is immediate, with no confirmation: the dialog closes at once
  // and the Undo toast is the way back (R3). Reactivating is the same, mirrored:
  // every reversible act's toast carries Undo.
  // Toasts come from mutateAsync().then, never mutate's per-call onSuccess:
  // TanStack drops per-call callbacks once the page unmounts. With the dialog
  // already closed, a failure is a toast too.
  function onToggle(p: Pocket) {
    if (toggle.isPending) return;
    const name = p.pocket_display_name;
    editParam.close();
    toggle
      .mutateAsync({ id: p.id, body: { active: !p.active } })
      .then(() => {
        toastUndoable({
          message: t(p.active ? "toast.deactivated" : "toast.reactivated", { name }),
          undo: () => updatePocket(p.id, { active: p.active }),
          qc,
          invalidate: pocketsKey,
        });
      })
      .catch((e: ApiError) => {
        toast.error(errorMessage(t, e));
      });
  }

  const all = pockets.data?.items;
  const inactiveCount = all?.filter((p) => !p.active).length ?? 0;
  const items = all && (showInactive ? all : all.filter((p) => p.active));
  const editing =
    all && editParam.value?.startsWith(EDIT_PREFIX)
      ? all.find((p) => p.id === editParam.value!.slice(EDIT_PREFIX.length))
      : undefined;
  // A link to a Pocket that is not there (deleted meanwhile, or a stale link)
  // drops the param rather than showing an empty dialog.
  const staleEdit = all && editParam.value !== null && !editing;

  const clearEdit = editParam.clear;
  useEffect(() => {
    if (staleEdit) clearEdit();
  }, [staleEdit, clearEdit]);

  return (
    <main className="mx-auto flex min-h-dvh w-full max-w-md flex-col gap-6 px-4 py-8">
      <header className="flex items-center justify-between gap-4">
        <h1 className="text-xl font-semibold">{t("pockets.title")}</h1>
        <Link to="/" className="inline-flex min-h-11 items-center text-sm underline">
          {t("pockets.back")}
        </Link>
      </header>
      {pockets.error ? (
        <ErrorState error={pockets.error} onRetry={() => void pockets.refetch()} />
      ) : !items ? (
        <Loading />
      ) : (
        <>
          {inactiveCount > 0 && (
            <label className="flex min-h-11 items-center justify-end gap-2 text-sm text-muted-foreground">
              <input
                type="checkbox"
                className="size-5"
                checked={showInactive}
                onChange={(e) => setShowInactive(e.target.checked)}
              />
              {t("pockets.showInactive", { count: inactiveCount })}
            </label>
          )}
          {items.length === 0 ? (
            <p className="text-muted-foreground">
              {inactiveCount > 0
                ? t("pockets.allInactive", { count: inactiveCount })
                : t("pockets.empty")}
            </p>
          ) : (
            <ul className="flex flex-col divide-y rounded-md border">
              {items.map((p) => (
                <li key={p.id}>
                  {/* The whole row opens its edit dialog, so no chevron. */}
                  <button
                    type="button"
                    aria-label={t(p.active ? "pockets.editFor" : "pockets.editForInactive", {
                      name: p.pocket_display_name,
                    })}
                    onClick={() => editParam.open(EDIT_PREFIX + p.id)}
                    className="flex min-h-11 w-full items-center gap-3 px-3 py-2 text-left hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  >
                    <span className={p.active ? "mr-auto" : "mr-auto text-muted-foreground"}>
                      {p.pocket_display_name}
                    </span>
                    {!p.active && (
                      <span className="rounded-md bg-muted px-2 py-0.5 text-xs text-muted-foreground">
                        {t("pockets.inactive")}
                      </span>
                    )}
                  </button>
                </li>
              ))}
            </ul>
          )}
          <Button className="w-full" onClick={() => newParam.open("pocket")}>
            {t("pockets.addHeading")}
          </Button>
        </>
      )}
      {newParam.value === "pocket" && (
        <Dialog open onOpenChange={(o) => !o && newParam.close()}>
          <DialogContent closeLabel={t("dialog.close")}>
            <PocketForm
              mode="create"
              attemptId={attemptId}
              newAttemptId={() => setAttemptId(uuidv7())}
              close={newParam.close}
            />
          </DialogContent>
        </Dialog>
      )}
      {editing && (
        <Dialog open onOpenChange={(o) => !o && editParam.close()}>
          <DialogContent closeLabel={t("dialog.close")}>
            <PocketForm
              key={editing.id}
              mode="edit"
              pocket={editing}
              close={editParam.close}
              onToggle={onToggle}
            />
          </DialogContent>
        </Dialog>
      )}
    </main>
  );
}

import { useEffect, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
import { restorePocket, updatePocket, type ApiError, type Pocket } from "@/api/client";
import {
  pocketsKey,
  useCreatePocket,
  useDeletePocket,
  usePockets,
  useUpdatePocket,
} from "@/api/queries";
import { errorMessage } from "@/api/errors";
import { BackLink } from "@/components/BackLink";
import { Badge } from "@/components/Badge";
import { CheckboxRow } from "@/components/CheckboxRow";
import { DetailActions } from "@/components/DetailActions";
import { Field, Form } from "@/components/Form";
import { ListCard, ListRow } from "@/components/List";
import { ErrorState, Loading } from "@/components/states";
import { EmptyState, FieldError, PageTitle } from "@/components/Text";
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
import { toastError, toastPlain, toastSaved } from "@/lib/plainToast";
import { routes } from "@/lib/routes";
import { toastUndoable } from "@/lib/undoToast";
import { useDialogParam } from "@/lib/useDialogParam";
import { uuidv7 } from "@/lib/uuid";

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
  onDelete?: (p: Pocket) => void;
};

function PocketForm({
  mode,
  pocket,
  attemptId,
  newAttemptId,
  close,
  onToggle,
  onDelete,
}: PocketFormProps) {
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
          toastPlain(t("toast.added", { name: p.pocket_display_name }));
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
        toastSaved();
        close();
      })
      .catch(() => {});
  }

  return (
    <Form onSubmit={onSubmit}>
      <DialogHeader>
        <DialogTitle>
          {t(mode === "create" ? "pockets.addHeading" : "pockets.editHeading")}
        </DialogTitle>
      </DialogHeader>
      <Field>
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
        {fieldMessage && <FieldError id="pocket-name-error">{fieldMessage}</FieldError>}
      </Field>
      {formMessage && <FieldError>{formMessage}</FieldError>}
      {mode === "edit" && (
        <DetailActions
          actions={[
            {
              key: "toggle",
              label: t(pocket!.active ? "pockets.deactivate" : "pockets.reactivate"),
              disabled: pending,
              onClick: () => onToggle?.(pocket!),
            },
            {
              key: "delete",
              label: t("pockets.delete"),
              tone: "destructive",
              disabled: pending,
              onClick: () => onDelete?.(pocket!),
            },
          ]}
        />
      )}
      <DialogFooter>
        {mode === "create" && (
          <Button type="button" variant="outline" disabled={pending} onClick={close}>
            {t("pockets.cancel")}
          </Button>
        )}
        <Button type="submit" disabled={pending || unchanged}>
          {pending
            ? t(mode === "create" ? "pockets.adding" : "pockets.saving")
            : t(mode === "create" ? "pockets.add" : "pockets.save")}
        </Button>
      </DialogFooter>
    </Form>
  );
}

export function PocketsPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const pockets = usePockets();
  const toggle = useUpdatePocket();
  const remove = useDeletePocket();
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
        toastError(errorMessage(t, e));
      });
  }

  // Delete is immediate, with no confirmation, and its Undo restores (R3, P6).
  function onDelete(p: Pocket) {
    if (remove.isPending) return;
    const name = p.pocket_display_name;
    editParam.close();
    remove
      .mutateAsync(p.id)
      .then(() => {
        toastUndoable({
          message: t("toast.deleted", { name }),
          undo: () => restorePocket(p.id),
          qc,
          invalidate: pocketsKey,
        });
      })
      .catch((e: ApiError) => {
        toastError(errorMessage(t, e));
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
    <>
      <BackLink to={routes.home} label={t("home.title")} />
      <PageTitle>{t("pockets.title")}</PageTitle>
      {pockets.error ? (
        <ErrorState error={pockets.error} onRetry={() => void pockets.refetch()} />
      ) : !items ? (
        <Loading />
      ) : (
        <>
          {inactiveCount > 0 && (
            <CheckboxRow checked={showInactive} onChange={setShowInactive}>
              {t("pockets.showInactive", { count: inactiveCount })}
            </CheckboxRow>
          )}
          {items.length === 0 ? (
            <EmptyState>
              {inactiveCount > 0
                ? t("pockets.allInactive", { count: inactiveCount })
                : t("pockets.empty")}
            </EmptyState>
          ) : (
            <ListCard>
              {items.map((p) => (
                // The whole row opens its edit dialog, so no chevron.
                <ListRow
                  key={p.id}
                  label={t(p.active ? "pockets.editFor" : "pockets.editForInactive", {
                    name: p.pocket_display_name,
                  })}
                  onClick={() => editParam.open(EDIT_PREFIX + p.id)}
                  muted={!p.active}
                  badge={!p.active && <Badge>{t("pockets.inactive")}</Badge>}
                >
                  {p.pocket_display_name}
                </ListRow>
              ))}
            </ListCard>
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
              onDelete={onDelete}
            />
          </DialogContent>
        </Dialog>
      )}
    </>
  );
}

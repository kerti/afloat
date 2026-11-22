import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { updatePocket, type ApiError, type Pocket } from "@/api/client";
import { pocketsKey, useCreatePocket, usePockets, useUpdatePocket } from "@/api/queries";
import { errorMessage } from "@/api/errors";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { toastUndoable } from "@/lib/undoToast";
import { uuidv7 } from "@/lib/uuid";

function PocketRow({ pocket, onToggle }: { pocket: Pocket; onToggle: (p: Pocket) => void }) {
  const { t } = useTranslation();
  const update = useUpdatePocket();
  // Null until typed, so only a changed name is sent.
  const [draft, setDraft] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);
  const name = pocket.pocket_display_name;
  const err = update.error;
  const onInput =
    err?.code === "NAME_TAKEN" ||
    (err?.code === "VALIDATION" && err.args.field === "pocket_display_name");

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (update.isPending || draft === null) return;
    if (draft === name) {
      setDraft(null);
      return;
    }
    update.mutate(
      { id: pocket.id, body: { pocket_display_name: draft } },
      {
        onSuccess: () => {
          setDraft(null);
          setSaved(true);
        },
      },
    );
  }

  if (draft !== null) {
    return (
      <li className="px-3 py-2">
        <form onSubmit={onSubmit} className="flex flex-col gap-2" noValidate>
          <Label htmlFor={`pocket-name-${pocket.id}`}>{t("fields.pocketDisplayName")}</Label>
          <Input
            id={`pocket-name-${pocket.id}`}
            value={draft}
            maxLength={60}
            autoFocus
            onChange={(e) => setDraft(e.target.value)}
            aria-invalid={onInput ? true : undefined}
            aria-describedby={err ? `pocket-error-${pocket.id}` : undefined}
          />
          {err && (
            <p id={`pocket-error-${pocket.id}`} role="alert" className="text-sm text-destructive">
              {errorMessage(t, err)}
            </p>
          )}
          <div className="flex gap-2">
            <Button type="submit" size="sm" disabled={update.isPending}>
              {update.isPending ? t("pockets.saving") : t("pockets.save")}
            </Button>
            <Button
              type="button"
              size="sm"
              variant="ghost"
              onClick={() => {
                update.reset();
                setDraft(null);
              }}
            >
              {t("pockets.cancel")}
            </Button>
          </div>
        </form>
      </li>
    );
  }

  return (
    <li className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2">
      <span className={pocket.active ? "mr-auto" : "mr-auto text-muted-foreground"}>{name}</span>
      {!pocket.active && (
        <span className="rounded-md bg-muted px-2 py-0.5 text-xs text-muted-foreground">
          {t("pockets.inactive")}
        </span>
      )}
      {saved && (
        <span role="status" className="text-sm text-muted-foreground">
          {t("pockets.saved")}
        </span>
      )}
      <Button
        size="sm"
        variant="ghost"
        aria-label={t("pockets.editFor", { name })}
        onClick={() => {
          setSaved(false);
          update.reset();
          setDraft(name);
        }}
      >
        {t("pockets.edit")}
      </Button>
      <Button
        size="sm"
        variant="ghost"
        aria-label={t(pocket.active ? "pockets.deactivateFor" : "pockets.reactivateFor", { name })}
        onClick={() => onToggle(pocket)}
      >
        {t(pocket.active ? "pockets.deactivate" : "pockets.reactivate")}
      </Button>
    </li>
  );
}

export function PocketsPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const pockets = usePockets();
  const toggle = useUpdatePocket();
  // Page state, not stored: unticked again on every visit (R7).
  const [showInactive, setShowInactive] = useState(false);
  const create = useCreatePocket();
  const [name, setName] = useState("");
  // One id per attempt: replaced only after a success, so a retry after a lost
  // response resends it and meets the server's 200 replay, never a duplicate.
  const [id, setId] = useState(uuidv7);

  const err = create.error;
  const onInput =
    err?.code === "NAME_TAKEN" ||
    (err?.code === "VALIDATION" && err.args.field === "pocket_display_name");

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (create.isPending) return;
    create.mutate(
      { id, pocket_display_name: name },
      {
        onSuccess: () => {
          setName("");
          setId(uuidv7());
        },
        // ID_TAKEN means nothing was written under this id for this caller, so
        // resending it can only fail again: the retry gets a fresh one.
        onError: (e) => {
          if (e.code === "ID_TAKEN") setId(uuidv7());
        },
      },
    );
  }

  // Deactivating is immediate, with no confirmation; the Undo toast is the way
  // back, and Reactivate on the row does not depend on it (R3).
  function onToggle(p: Pocket) {
    if (toggle.isPending) return;
    // mutateAsync, not mutate's per-call onSuccess: TanStack drops per-call
    // callbacks once the page unmounts, and leaving mid-request must still
    // raise the Undo toast. Failure is shown from toggle.error.
    toggle
      .mutateAsync({ id: p.id, body: { active: !p.active } })
      .then(() => {
        if (!p.active) return;
        toastUndoable({
          message: t("toast.deactivated"),
          undo: () => updatePocket(p.id, { active: true }),
          qc,
          invalidate: pocketsKey,
        });
      })
      .catch(() => {});
  }

  const all = pockets.data?.items;
  const inactiveCount = all?.filter((p) => !p.active).length ?? 0;
  const items = all && (showInactive ? all : all.filter((p) => p.active));
  const toggleErr: ApiError | null = toggle.error;

  return (
    <main className="mx-auto flex min-h-dvh w-full max-w-md flex-col gap-6 px-4 py-8">
      <header className="flex items-center justify-between gap-4">
        <h1 className="text-xl font-semibold">{t("pockets.title")}</h1>
        <Link to="/" className="text-sm underline">
          {t("pockets.back")}
        </Link>
      </header>
      {pockets.error ? (
        <p role="alert" className="text-sm text-destructive">
          {errorMessage(t, pockets.error)}
        </p>
      ) : items ? (
        <>
          {inactiveCount > 0 && (
            <label className="flex items-center justify-end gap-2 text-sm text-muted-foreground">
              <input
                type="checkbox"
                className="h-4 w-4"
                checked={showInactive}
                onChange={(e) => setShowInactive(e.target.checked)}
              />
              {t("pockets.showInactive", { count: inactiveCount })}
            </label>
          )}
          {toggleErr && (
            <p role="alert" className="text-sm text-destructive">
              {errorMessage(t, toggleErr)}
            </p>
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
                <PocketRow key={p.id} pocket={p} onToggle={onToggle} />
              ))}
            </ul>
          )}
        </>
      ) : null}
      <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
        <h2 className="text-base font-semibold">{t("pockets.addHeading")}</h2>
        <div className="flex flex-col gap-2">
          <Label htmlFor="pocket-display-name">{t("fields.pocketDisplayName")}</Label>
          <Input
            id="pocket-display-name"
            value={name}
            maxLength={60}
            placeholder={t("pockets.namePlaceholder")}
            onChange={(e) => setName(e.target.value)}
            aria-invalid={onInput ? true : undefined}
            aria-describedby={err ? "pocket-error" : undefined}
          />
        </div>
        {err && (
          <p id="pocket-error" role="alert" className="text-sm text-destructive">
            {errorMessage(t, err)}
          </p>
        )}
        <Button type="submit" disabled={create.isPending}>
          {create.isPending ? t("pockets.adding") : t("pockets.add")}
        </Button>
      </form>
    </main>
  );
}

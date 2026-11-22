import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import { useCreatePocket, usePockets } from "@/api/queries";
import { errorMessage } from "@/api/errors";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { uuidv7 } from "@/lib/uuid";

export function PocketsPage() {
  const { t } = useTranslation();
  const pockets = usePockets();
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

  const items = pockets.data?.items;

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
      ) : items && items.length === 0 ? (
        <p className="text-muted-foreground">{t("pockets.empty")}</p>
      ) : items ? (
        <ul className="flex flex-col divide-y rounded-md border">
          {items.map((p) => (
            <li key={p.id} className="px-3 py-2">
              {p.pocket_display_name}
            </li>
          ))}
        </ul>
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

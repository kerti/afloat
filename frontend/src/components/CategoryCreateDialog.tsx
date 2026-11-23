import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { useCreateCategory } from "@/api/queries";
import type { Bucket } from "@/api/client";
import { errorMessage } from "@/api/errors";
import { Field, Form } from "@/components/Form";
import { MoneyInput } from "@/components/MoneyInput";
import { FieldError, FieldHint } from "@/components/Text";
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
import { Select, SelectContent, SelectItem, SelectTrigger } from "@/components/ui/select";
import { toastPlain } from "@/lib/plainToast";

type FieldKey = "name" | "bucket" | "allocation";

// The Category create dialog. An allocation is a plan, not a record of money,
// so a tap outside closes it like any other dialog (ui-conventions, Outside tap).
export function CategoryCreateDialog({
  currency,
  attemptId,
  newAttemptId,
  close,
}: {
  currency: string;
  attemptId: string;
  newAttemptId: () => void;
  close: () => void;
}) {
  const { t } = useTranslation();
  const create = useCreateCategory();
  const [name, setName] = useState("");
  const [bucket, setBucket] = useState<Bucket | "">("");
  const [allocation, setAllocation] = useState("");
  // Touched means edited then left: a blur alone (the outside tap that closes
  // the dialog) must not flash an error (ui-conventions, Forms).
  const [edited, setEdited] = useState<Partial<Record<FieldKey, boolean>>>({});
  const [touched, setTouched] = useState<Partial<Record<FieldKey, boolean>>>({});
  const [submitted, setSubmitted] = useState(false);

  const pending = create.isPending;
  const err = create.error;
  const shows = (k: FieldKey) => touched[k] || submitted;
  const local: Record<FieldKey, string | null> = {
    name: name.trim() === "" ? t("categories.nameRequired") : null,
    bucket: bucket === "" ? t("categories.kindRequired") : null,
    allocation:
      bucket === "commitment" && allocation === "" ? t("categories.allocationRequired") : null,
  };
  const serverField: FieldKey | null = !err
    ? null
    : err.code === "NAME_TAKEN"
      ? "name"
      : err.code === "VALIDATION"
        ? (({ category_display_name: "name", bucket: "bucket", allocation: "allocation" } as const)[
            err.args.field as string
          ] ?? null)
        : null;
  const message = (k: FieldKey) =>
    err && serverField === k ? errorMessage(t, err) : shows(k) ? local[k] : null;
  const formMessage = err && serverField === null ? errorMessage(t, err) : null;

  const clearServer = () => {
    if (err) create.reset();
  };
  const touch = (k: FieldKey) => setTouched((p) => ({ ...p, [k]: true }));
  const edit = (k: FieldKey) => setEdited((p) => ({ ...p, [k]: true }));
  const leave = (k: FieldKey) => edited[k] && touch(k);

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (pending) return;
    setSubmitted(true);
    if (local.name || local.bucket || local.allocation) return;
    create
      .mutateAsync({
        id: attemptId,
        category_display_name: name,
        bucket: bucket as Bucket,
        ...(allocation !== "" && { allocation }),
      })
      .then((c) => {
        // Replaced only after a success, so a retry after a lost response
        // resends the id and meets the server's 200 replay.
        newAttemptId();
        toastPlain(t("toast.added", { name: c.category_display_name }));
        close();
      })
      .catch((e: unknown) => {
        if ((e as { code?: string }).code === "ID_TAKEN") newAttemptId();
      });
  }

  const nameMessage = message("name");
  const bucketMessage = message("bucket");
  const allocationMessage = message("allocation");

  return (
    <Dialog open onOpenChange={(o) => !o && close()}>
      <DialogContent closeLabel={t("dialog.close")}>
        <Form onSubmit={onSubmit}>
          <DialogHeader>
            <DialogTitle>{t("categories.addHeading")}</DialogTitle>
          </DialogHeader>
          <Field>
            <Label htmlFor="category-display-name">{t("fields.categoryDisplayName")}</Label>
            <Input
              id="category-display-name"
              value={name}
              maxLength={60}
              placeholder={t("categories.namePlaceholder")}
              disabled={pending}
              onChange={(e) => {
                setName(e.target.value);
                edit("name");
                clearServer();
              }}
              onBlur={() => leave("name")}
              aria-invalid={nameMessage ? true : undefined}
              aria-describedby={nameMessage ? "category-name-error" : undefined}
            />
            {nameMessage && <FieldError id="category-name-error">{nameMessage}</FieldError>}
          </Field>
          <Field>
            <Label htmlFor="category-bucket">{t("fields.categoryBucket")}</Label>
            <Select
              value={bucket}
              disabled={pending}
              onValueChange={(v) => {
                setBucket(v as Bucket);
                touch("bucket");
                clearServer();
              }}
            >
              <SelectTrigger
                id="category-bucket"
                aria-invalid={bucketMessage ? true : undefined}
                aria-describedby={bucketMessage ? "category-bucket-error" : undefined}
              />
              <SelectContent>
                <SelectItem value="daily">{t("categories.kindDaily")}</SelectItem>
                <SelectItem value="commitment">{t("categories.kindCommitment")}</SelectItem>
              </SelectContent>
            </Select>
            {bucketMessage && <FieldError id="category-bucket-error">{bucketMessage}</FieldError>}
          </Field>
          <Field>
            <Label htmlFor="category-allocation">{t("fields.allocation")}</Label>
            <MoneyInput
              id="category-allocation"
              currency={currency}
              value={allocation}
              disabled={pending}
              onChange={(wire) => {
                setAllocation(wire);
                edit("allocation");
                clearServer();
              }}
              onBlur={() => leave("allocation")}
              aria-invalid={allocationMessage ? true : undefined}
              aria-describedby={allocationMessage ? "category-allocation-error" : undefined}
            />
            {allocationMessage ? (
              <FieldError id="category-allocation-error">{allocationMessage}</FieldError>
            ) : (
              <FieldHint>
                {t(
                  bucket === "commitment"
                    ? "categories.allocationCommitmentHint"
                    : "categories.allocationDailyHint",
                )}
              </FieldHint>
            )}
          </Field>
          {formMessage && <FieldError>{formMessage}</FieldError>}
          <DialogFooter>
            <Button type="button" variant="outline" disabled={pending} onClick={close}>
              {t("categories.cancel")}
            </Button>
            <Button type="submit" disabled={pending}>
              {pending ? t("categories.adding") : t("categories.add")}
            </Button>
          </DialogFooter>
        </Form>
      </DialogContent>
    </Dialog>
  );
}

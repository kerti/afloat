import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
import { budgetKey, categoriesKey, useUpdateBudget, useUpdateCategory } from "@/api/queries";
import type { ApiError, Category } from "@/api/client";
import { errorMessage } from "@/api/errors";
import { DetailActions } from "@/components/DetailActions";
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
import { canonicalMoney, roundMoney } from "@/lib/money";
import { clampName } from "@/lib/codePoints";
import { toastError, toastSaved } from "@/lib/plainToast";

type FieldKey = "name" | "allocation";

// A server field path, bare or nested in the allocations list, to the form's field.
function serverFieldOf(err: ApiError | null): FieldKey | null {
  if (!err) return null;
  if (err.code === "NAME_TAKEN") return "name";
  if (err.code !== "VALIDATION") return null;
  const field = typeof err.args.field === "string" ? err.args.field : "";
  if (field === "category_display_name") return "name";
  return /^(allocations\.\d+\.)?allocation$/.test(field) ? "allocation" : null;
}

// A Category that no longer exists: NOT_FOUND from its own PATCH, or the
// budget PATCH's {category_id, invalid}.
function isGone(err: ApiError): boolean {
  if (err.code === "NOT_FOUND") return true;
  const field = typeof err.args.field === "string" ? err.args.field : "";
  return (
    err.code === "VALIDATION" &&
    err.args.rule === "invalid" &&
    /^(allocations\.\d+\.)?category_id$/.test(field)
  );
}

// The Category edit dialog: its name and its allocation for the current Period,
// and the record-level act (Deactivate or Reactivate). Its Kind is shown, never
// changed. A name is the Category's (PATCH /categories/{id}); an allocation is the
// Budget's (PATCH /budget), so Save sends only the parts that changed, the name
// first. If the name saves and the allocation then fails, the name stays saved,
// the dialog stays open with the error, and a retry sends the allocation alone.
export function CategoryEditDialog({
  category,
  stored,
  currency,
  close,
  onToggle,
  onDelete,
}: {
  category: Category;
  // The Category's allocation in the current Period, a wire amount, or undefined
  // when it has none.
  stored: string | undefined;
  currency: string;
  close: () => void;
  onToggle: (c: Category) => void;
  // Delete waits for the answer: it resolves once the Category is deleted (the
  // page closes the dialog and raises the Undo toast) and rejects with the ApiError.
  onDelete: (c: Category) => Promise<void>;
}) {
  const { t } = useTranslation();
  const updateCategory = useUpdateCategory();
  const updateBudget = useUpdateBudget();
  const qc = useQueryClient();
  const commitment = category.bucket === "commitment";
  // Initialised from the record, never reset in an effect.
  const shownStored = roundMoney(stored ?? "", currency);
  const [name, setName] = useState(category.category_display_name);
  const [allocation, setAllocation] = useState(shownStored);
  // Touched means edited then left: a blur alone (the outside tap that closes
  // the dialog) must not flash an error (ui-conventions, Forms).
  const [edited, setEdited] = useState<Partial<Record<FieldKey, boolean>>>({});
  const [touched, setTouched] = useState<Partial<Record<FieldKey, boolean>>>({});
  const [submitted, setSubmitted] = useState(false);
  const [pending, setPending] = useState(false);
  const [err, setErr] = useState<ApiError | null>(null);
  const [deleting, setDeleting] = useState(false);

  const nameChanged = name.trim() !== category.category_display_name;
  const allocationChanged = canonicalMoney(allocation) !== canonicalMoney(shownStored);
  const unchanged = !nameChanged && !allocationChanged;
  const local: Record<FieldKey, string | null> = {
    name: name.trim() === "" ? t("categories.nameRequired") : null,
    allocation: commitment && allocation === "" ? t("categories.allocationRequired") : null,
  };
  const serverField = serverFieldOf(err);
  const message = (k: FieldKey) =>
    err && serverField === k ? errorMessage(t, err) : touched[k] || submitted ? local[k] : null;
  const formMessage = err && serverField === null ? errorMessage(t, err) : null;
  const nameMessage = message("name");
  const allocationMessage = message("allocation");

  const edit = (k: FieldKey) => {
    setEdited((p) => ({ ...p, [k]: true }));
    setErr(null);
  };
  const leave = (k: FieldKey) => {
    if (edited[k]) setTouched((p) => ({ ...p, [k]: true }));
  };

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (pending || unchanged) return;
    setSubmitted(true);
    if (local.name || local.allocation) return;
    setPending(true);
    setErr(null);
    try {
      if (nameChanged) {
        await updateCategory.mutateAsync({
          id: category.id,
          body: { category_display_name: name },
        });
      }
      if (allocationChanged) {
        // "0" is an allocation; only a Daily Category may be cleared, to null.
        await updateBudget.mutateAsync({
          allocations: [
            { category_id: category.id, allocation: allocation === "" ? null : allocation },
          ],
        });
      }
      toastSaved();
      close();
    } catch (e) {
      const failure = e as ApiError;
      if (isGone(failure)) {
        // The Category is gone, so there is no form left to hold the message:
        // say it as a toast, refetch both reads (the Category drops out) and
        // close. PATCH /budget answers a deleted Category as VALIDATION
        // {category_id, invalid}, the same answer as for an unknown or foreign one.
        toastError(t("errors.NOT_FOUND"));
        void qc.invalidateQueries({ queryKey: categoriesKey });
        void qc.invalidateQueries({ queryKey: budgetKey });
        close();
      } else {
        setErr(failure);
      }
    } finally {
      setPending(false);
    }
  }

  // Unlike Deactivate, Delete can be refused (CATEGORY_IN_USE): the dialog stays
  // open with the error above the footer and Deactivate beside it. A Category
  // that is already gone is a toast and a close, as for a failed Save.
  async function onDeleteClick() {
    if (pending) return;
    setPending(true);
    setDeleting(true);
    setErr(null);
    try {
      await onDelete(category);
    } catch (e) {
      const failure = e as ApiError;
      if (failure.code === "NOT_FOUND") {
        toastError(errorMessage(t, failure));
        close();
      } else {
        setErr(failure);
      }
    } finally {
      setPending(false);
      setDeleting(false);
    }
  }

  return (
    <Dialog open onOpenChange={(o) => !o && !deleting && close()}>
      <DialogContent closeLabel={t("dialog.close")}>
        <Form onSubmit={(e) => void onSubmit(e)}>
          <DialogHeader>
            <DialogTitle>{t("categories.editHeading")}</DialogTitle>
          </DialogHeader>
          <Field>
            <Label htmlFor="category-display-name">{t("fields.categoryDisplayName")}</Label>
            <Input
              id="category-display-name"
              value={name}
              disabled={pending}
              onChange={(e) => {
                setName(clampName(e.target.value));
                edit("name");
              }}
              onBlur={() => leave("name")}
              aria-invalid={nameMessage ? true : undefined}
              aria-describedby={nameMessage ? "category-name-error" : undefined}
            />
            {nameMessage && <FieldError id="category-name-error">{nameMessage}</FieldError>}
          </Field>
          <Field>
            <Label htmlFor="category-bucket">{t("fields.categoryBucket")}</Label>
            <Select value={category.bucket} disabled>
              <SelectTrigger id="category-bucket" />
              <SelectContent>
                <SelectItem value="daily">{t("categories.kindDaily")}</SelectItem>
                <SelectItem value="commitment">{t("categories.kindCommitment")}</SelectItem>
              </SelectContent>
            </Select>
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
                  commitment
                    ? "categories.allocationCommitmentHint"
                    : "categories.allocationDailyEditHint",
                )}
              </FieldHint>
            )}
          </Field>
          {formMessage && <FieldError>{formMessage}</FieldError>}
          <DetailActions
            actions={[
              {
                key: "toggle",
                label: t(category.active ? "categories.deactivate" : "categories.reactivate"),
                disabled: pending,
                onClick: () => onToggle(category),
              },
              {
                key: "delete",
                label: t("categories.delete"),
                tone: "destructive",
                disabled: pending,
                onClick: () => void onDeleteClick(),
              },
            ]}
          />
          <DialogFooter>
            <Button type="submit" disabled={pending || unchanged}>
              {pending ? t("categories.saving") : t("categories.save")}
            </Button>
          </DialogFooter>
        </Form>
      </DialogContent>
    </Dialog>
  );
}

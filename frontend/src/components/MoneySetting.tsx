import { useState, type FormEvent, type KeyboardEvent } from "react";
import { useTranslation } from "react-i18next";
import type { ApiError } from "@/api/client";
import { errorMessage } from "@/api/errors";
import { Field, Form } from "@/components/Form";
import { MoneyInput } from "@/components/MoneyInput";
import { FieldError, FieldHint } from "@/components/Text";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { canonicalMoney, roundMoney } from "@/lib/money";
import { toastPlain, toastSaved } from "@/lib/plainToast";

// A typed money setting: TextSetting's behaviour (its own Save, enabled once the
// amount differs from the stored one; Enter saves, Esc reverts; a failure shows
// inline and typing clears it) over a MoneyInput. A blank amount cannot be saved:
// a Monthly Budget cannot be cleared. PROPOSAL pending the phone preview, #130
// C15. `stored` is the wire amount, or undefined when none is set yet.
// `clearable` lets a blank amount be saved, as null (an optional amount, the
// expected monthly income). `save` may resolve to the sentence its toast says
// in place of "Saved."; `hint` is the FieldHint under the field.
export function MoneySetting({
  id,
  label,
  currency,
  stored,
  save,
  pending,
  error,
  clearError,
  clearable = false,
  hint,
}: {
  id: string;
  label: string;
  currency: string;
  stored: string | undefined;
  save: (next: string | null) => Promise<string | void>;
  pending: boolean;
  error: ApiError | null;
  clearError: () => void;
  clearable?: boolean;
  hint?: string;
}) {
  const { t } = useTranslation();
  const [draft, setDraft] = useState<string | null>(null);
  // Loaded as the figures show it: rounded half up to the currency's decimals.
  const shownStored = roundMoney(stored ?? "", currency);
  const changed =
    draft !== null &&
    (clearable || draft !== "") &&
    canonicalMoney(draft) !== canonicalMoney(shownStored);

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (!changed || pending) return;
    save(draft === "" ? null : draft)
      .then((notice) => {
        setDraft(null);
        if (notice) toastPlain(notice);
        else toastSaved();
      })
      .catch(() => {});
  }

  function onKeyDown(e: KeyboardEvent) {
    if (e.key !== "Escape" || draft === null) return;
    setDraft(null);
    clearError();
  }

  const errorId = `${id}-error`;
  const hintId = `${id}-hint`;
  const describedBy = [hint ? hintId : null, error ? errorId : null].filter(Boolean).join(" ");
  return (
    <Form onSubmit={onSubmit}>
      <Field>
        <Label htmlFor={id}>{label}</Label>
        <div className="flex gap-2">
          <MoneyInput
            id={id}
            className="min-w-0 flex-1"
            currency={currency}
            value={draft ?? shownStored}
            disabled={pending}
            onChange={(wire) => {
              setDraft(wire);
              if (error) clearError();
            }}
            onKeyDown={onKeyDown}
            aria-invalid={error ? true : undefined}
            aria-describedby={describedBy || undefined}
          />
          <Button type="submit" disabled={!changed || pending}>
            {pending ? t("settings.saving") : t("settings.save")}
          </Button>
        </div>
        {hint && <FieldHint id={hintId}>{hint}</FieldHint>}
        {error && <FieldError id={errorId}>{errorMessage(t, error)}</FieldError>}
      </Field>
    </Form>
  );
}

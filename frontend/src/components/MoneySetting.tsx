import { useState, type FormEvent, type KeyboardEvent } from "react";
import { useTranslation } from "react-i18next";
import type { ApiError } from "@/api/client";
import { errorMessage } from "@/api/errors";
import { Field, Form } from "@/components/Form";
import { MoneyInput } from "@/components/MoneyInput";
import { FieldError } from "@/components/Text";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { canonicalMoney, roundMoney } from "@/lib/money";
import { toastSaved } from "@/lib/plainToast";

// A typed money setting: TextSetting's behaviour (its own Save, enabled once the
// amount differs from the stored one; Enter saves, Esc reverts; a failure shows
// inline and typing clears it) over a MoneyInput. A blank amount cannot be saved:
// a Monthly Budget cannot be cleared. PROPOSAL pending the phone preview, #130
// C15. `stored` is the wire amount, or undefined when none is set yet.
export function MoneySetting({
  id,
  label,
  currency,
  stored,
  save,
  pending,
  error,
  clearError,
}: {
  id: string;
  label: string;
  currency: string;
  stored: string | undefined;
  save: (next: string) => Promise<unknown>;
  pending: boolean;
  error: ApiError | null;
  clearError: () => void;
}) {
  const { t } = useTranslation();
  const [draft, setDraft] = useState<string | null>(null);
  // Loaded as the figures show it: rounded half up to the currency's decimals.
  const shownStored = roundMoney(stored ?? "", currency);
  const changed =
    draft !== null && draft !== "" && canonicalMoney(draft) !== canonicalMoney(shownStored);

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (!changed || pending) return;
    save(draft)
      .then(() => {
        setDraft(null);
        toastSaved();
      })
      .catch(() => {});
  }

  function onKeyDown(e: KeyboardEvent) {
    if (e.key !== "Escape" || draft === null) return;
    setDraft(null);
    clearError();
  }

  const errorId = `${id}-error`;
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
            aria-describedby={error ? errorId : undefined}
          />
          <Button type="submit" disabled={!changed || pending}>
            {pending ? t("settings.saving") : t("settings.save")}
          </Button>
        </div>
        {error && <FieldError id={errorId}>{errorMessage(t, error)}</FieldError>}
      </Field>
    </Form>
  );
}

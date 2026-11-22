import { useState, type FormEvent, type KeyboardEvent } from "react";
import { useTranslation } from "react-i18next";
import type { ApiError } from "@/api/client";
import { errorMessage } from "@/api/errors";
import { Field, Form } from "@/components/Form";
import { FieldError } from "@/components/Text";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { toastSaved } from "@/lib/plainToast";

// A typed setting keeps an explicit Save, enabled once the text differs from
// the stored value (trimmed, so a trailing space does not enable it). Enter
// saves, Esc reverts, and a failure shows inline under the field
// (docs/agents/ui-conventions.md, Settings save by control kind; Balances
// ADR-0058 §7, github.com/kerti/balances-v2). The draft is null until typed,
// so a background /me refetch never overwrites what is being edited.
export function TextSetting({
  id,
  label,
  stored,
  autoComplete,
  save,
  pending,
  error,
  clearError,
}: {
  id: string;
  label: string;
  stored: string;
  autoComplete?: string;
  save: (next: string) => Promise<unknown>;
  pending: boolean;
  error: ApiError | null;
  clearError: () => void;
}) {
  const { t } = useTranslation();
  const [draft, setDraft] = useState<string | null>(null);
  const changed = draft !== null && draft.trim() !== stored;

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (!changed || pending) return;
    // The failure is shown from `error`, so it is swallowed here.
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
          <Input
            id={id}
            className="min-w-0 flex-1"
            autoComplete={autoComplete}
            value={draft ?? stored}
            disabled={pending}
            onChange={(e) => {
              setDraft(e.target.value);
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

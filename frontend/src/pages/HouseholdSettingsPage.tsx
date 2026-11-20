import { useMemo, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import type { Me, UpdateHouseholdRequest } from "@/api/client";
import { useUpdateHousehold } from "@/api/queries";
import { errorMessage } from "@/api/errors";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import currencyFixture from "../../../contract/testdata/currency.json";

const days = Array.from({ length: 28 }, (_, i) => i + 1);

const selectClass =
  "h-9 w-full rounded-md border border-input bg-transparent px-3 text-sm shadow-xs aria-invalid:border-destructive";

// Each field is null until typed, so a background /me refetch never overwrites
// what is being edited, and only what changed is sent (the PATCH merges).
export function HouseholdSettingsPage({ me }: { me: Me }) {
  const { t, i18n } = useTranslation();
  const update = useUpdateHousehold();
  const h = me.household;
  const [name, setName] = useState<string | null>(null);
  const [currency, setCurrency] = useState<string | null>(null);
  const [day, setDay] = useState<number | null>(null);

  const currencies = useMemo(() => {
    const set = new Set([...currencyFixture.codes, h.reporting_currency]);
    let names: Intl.DisplayNames | null = null;
    try {
      names = new Intl.DisplayNames([i18n.language], { type: "currency" });
    } catch {
      // Names are decoration; the code is the value.
    }
    return [...set].sort().map((code) => ({ code, name: names?.of(code) ?? code }));
  }, [h.reporting_currency, i18n.language]);

  const body: UpdateHouseholdRequest = {};
  if (name !== null && name !== h.household_display_name) body.household_display_name = name;
  if (currency !== null && currency !== h.reporting_currency) body.reporting_currency = currency;
  if (day !== null && day !== h.period_start_day) body.period_start_day = day;
  const changed = Object.keys(body).length > 0;

  const invalidField = update.error?.code === "VALIDATION" ? update.error.args.field : undefined;
  const invalid = (field: string) => (invalidField === field ? true : undefined);

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (!changed) return;
    update.mutate(body, {
      onSuccess: () => {
        setName(null);
        setCurrency(null);
        setDay(null);
      },
    });
  }

  return (
    <main className="mx-auto flex min-h-dvh w-full max-w-md flex-col gap-6 px-4 py-8">
      <header className="flex items-center justify-between gap-4">
        <h1 className="text-xl font-semibold">{t("settings.household.title")}</h1>
        <Link to="/" className="text-sm underline">
          {t("settings.household.back")}
        </Link>
      </header>
      <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
        <div className="flex flex-col gap-2">
          <Label htmlFor="household-display-name">{t("fields.householdDisplayName")}</Label>
          <Input
            id="household-display-name"
            value={name ?? h.household_display_name}
            onChange={(e) => setName(e.target.value)}
            aria-invalid={invalid("household_display_name")}
          />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="reporting-currency">{t("fields.reportingCurrency")}</Label>
          <select
            id="reporting-currency"
            className={selectClass}
            value={currency ?? h.reporting_currency}
            onChange={(e) => setCurrency(e.target.value)}
            aria-invalid={invalid("reporting_currency")}
          >
            {currencies.map((c) => (
              <option key={c.code} value={c.code}>
                {c.code} · {c.name}
              </option>
            ))}
          </select>
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="period-start-day">{t("fields.periodStartDay")}</Label>
          <select
            id="period-start-day"
            className={selectClass}
            value={day ?? h.period_start_day}
            onChange={(e) => setDay(Number(e.target.value))}
            aria-invalid={invalid("period_start_day")}
            aria-describedby="period-start-day-hint"
          >
            {days.map((d) => (
              <option key={d} value={d}>
                {d}
              </option>
            ))}
          </select>
          <p id="period-start-day-hint" className="text-sm text-muted-foreground">
            {t("settings.household.periodStartDayHint")}
          </p>
        </div>
        {update.error && (
          <p role="alert" className="text-sm text-destructive">
            {errorMessage(t, update.error)}
          </p>
        )}
        {update.isSuccess && !changed && (
          <p role="status" className="text-sm text-muted-foreground">
            {t("settings.household.saved")}
          </p>
        )}
        <Button type="submit" disabled={!changed || update.isPending}>
          {update.isPending ? t("settings.household.saving") : t("settings.household.save")}
        </Button>
      </form>
    </main>
  );
}

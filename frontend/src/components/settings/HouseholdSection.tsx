import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import type { Me, UpdateHouseholdRequest } from "@/api/client";
import { useUpdateHousehold } from "@/api/queries";
import { TextSetting } from "@/components/TextSetting";
import { Field, Section } from "@/components/Form";
import { FieldHint } from "@/components/Text";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger } from "@/components/ui/select";
import { SearchSelect } from "@/components/ui/search-select";
import { useChoice } from "@/lib/useChoice";
import currencyFixture from "../../../../contract/testdata/currency.json";

const allowanceModes = ["adaptive", "fixed"] as const;
const days = Array.from({ length: 28 }, (_, i) => i + 1);

export function HouseholdSection({ me }: { me: Me }) {
  const { t, i18n } = useTranslation();
  const h = me.household;
  const rename = useUpdateHousehold();
  const patch = useUpdateHousehold();
  const save = (body: UpdateHouseholdRequest) => patch.mutateAsync(body);
  const [currency, chooseCurrency] = useChoice(h.reporting_currency, (v) =>
    save({ reporting_currency: v }),
  );
  const [day, chooseDay] = useChoice(h.period_start_day, (v) => save({ period_start_day: v }));
  const [mode, chooseMode] = useChoice(h.allowance_mode, (v) => save({ allowance_mode: v }));

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

  return (
    <Section id="household" title={t("settings.household.title")}>
      <TextSetting
        id="household-display-name"
        label={t("fields.householdDisplayName")}
        stored={h.household_display_name}
        save={(name) => rename.mutateAsync({ household_display_name: name })}
        pending={rename.isPending}
        error={rename.error}
        clearError={rename.reset}
      />
      <Field>
        <Label htmlFor="reporting-currency">{t("fields.reportingCurrency")}</Label>
        <SearchSelect
          id="reporting-currency"
          value={currency}
          onValueChange={chooseCurrency}
          options={currencies.map((c) => ({ value: c.code, label: `${c.code} · ${c.name}` }))}
          searchLabel={t("settings.household.searchCurrencies")}
          noMatch={t("settings.household.noCurrency")}
        />
      </Field>
      <Field>
        <Label htmlFor="period-start-day">{t("fields.periodStartDay")}</Label>
        <Select value={String(day)} onValueChange={(v) => chooseDay(Number(v))}>
          <SelectTrigger id="period-start-day" aria-describedby="period-start-day-hint" />
          <SelectContent>
            {days.map((d) => (
              <SelectItem key={d} value={String(d)}>
                {d}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <FieldHint id="period-start-day-hint">
          {t("settings.household.periodStartDayHint")}
        </FieldHint>
      </Field>
      <Field>
        <Label htmlFor="allowance-mode">{t("fields.allowanceMode")}</Label>
        <Select
          value={mode}
          onValueChange={(v) => chooseMode(v as (typeof allowanceModes)[number])}
        >
          <SelectTrigger id="allowance-mode" aria-describedby="allowance-mode-hint" />
          <SelectContent>
            {allowanceModes.map((m) => (
              <SelectItem key={m} value={m}>
                {t(`settings.household.allowanceModes.${m}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <FieldHint id="allowance-mode-hint">{t("settings.household.allowanceModeHint")}</FieldHint>
      </Field>
    </Section>
  );
}

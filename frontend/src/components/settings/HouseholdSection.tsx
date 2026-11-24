import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import type { Me, UpdateHouseholdRequest } from "@/api/client";
import { useUpdateHousehold } from "@/api/queries";
import { TextSetting } from "@/components/TextSetting";
import { Field, Section } from "@/components/Form";
import { FieldHint } from "@/components/Text";
import { Label } from "@/components/ui/label";
import { NativeSelect } from "@/components/ui/select";
import { useChoice } from "@/lib/useChoice";
import currencyFixture from "../../../../contract/testdata/currency.json";

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
        <NativeSelect
          id="reporting-currency"
          value={currency}
          onChange={(e) => chooseCurrency(e.target.value)}
        >
          {currencies.map((c) => (
            <option key={c.code} value={c.code}>
              {c.code} · {c.name}
            </option>
          ))}
        </NativeSelect>
      </Field>
      <Field>
        <Label htmlFor="period-start-day">{t("fields.periodStartDay")}</Label>
        <NativeSelect
          id="period-start-day"
          value={day}
          onChange={(e) => chooseDay(Number(e.target.value))}
          aria-describedby="period-start-day-hint"
        >
          {days.map((d) => (
            <option key={d} value={d}>
              {d}
            </option>
          ))}
        </NativeSelect>
        <FieldHint id="period-start-day-hint">
          {t("settings.household.periodStartDayHint")}
        </FieldHint>
      </Field>
    </Section>
  );
}

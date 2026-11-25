import { useTranslation } from "react-i18next";
import { formatWeekdayDate, previousDay, yearOf } from "@/lib/dates";

// A day in the words the Expenses list heads it with: "Today", "Yesterday", else
// the weekday and date, the year only when it differs from today's. `today` is
// the server's (CLAUDE.md rule 5), never the device's. The edit route's date row
// reads the same words.
export function useDayHeading(today: string): (day: string) => string {
  const { t, i18n } = useTranslation();
  const yesterday = previousDay(today);
  return (day) =>
    day === today
      ? t("dates.today")
      : day === yesterday
        ? t("expenses.yesterday")
        : formatWeekdayDate(day, i18n.language, yearOf(day) !== yearOf(today));
}

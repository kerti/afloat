import { useTranslation } from "react-i18next";
import type { Me } from "@/api/client";
import { HouseholdSection } from "@/components/settings/HouseholdSection";
import { PocketsSection } from "@/components/settings/PocketsSection";
import { YouSection } from "@/components/settings/YouSection";
import { PageTitle } from "@/components/Text";

// One screen in three sections (ui-conventions, Layout and navigation). The
// Pocket dialogs are URL-driven (?new=pocket, ?edit=pocket:<id>) on this path.
export function SettingsPage({ me }: { me: Me }) {
  const { t } = useTranslation();

  return (
    <>
      <PageTitle>{t("settings.title")}</PageTitle>
      <HouseholdSection me={me} />
      <PocketsSection />
      <YouSection me={me} />
    </>
  );
}

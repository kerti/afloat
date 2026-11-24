import { useTranslation } from "react-i18next";
import type { Me } from "@/api/client";
import { BodyText, MutedText, PageTitle } from "@/components/Text";
import { TextLink } from "@/components/TextLink";
import { routes } from "@/lib/routes";

// The dashboard (the hero figure and the State) lands with the domain at
// BOOTSTRAP §11 step 9; until then this proves the session. The Shell around it
// carries the app name and sign-out.
export function HomePage({ me }: { me: Me }) {
  const { t } = useTranslation();

  return (
    <>
      <PageTitle>{t("home.title")}</PageTitle>
      <div className="flex flex-col gap-1">
        <BodyText>{t("home.signedInAs", { name: me.user_display_name })}</BodyText>
        <MutedText>{t("home.household", { name: me.household.household_display_name })}</MutedText>
      </div>
      <TextLink to={routes.settings}>{t("home.settingsLink")}</TextLink>
      <TextLink to={routes.budget}>{t("home.budgetLink")}</TextLink>
      <MutedText>{t("home.comingSoon")}</MutedText>
    </>
  );
}

import { useEffect, useState } from "react";
import { Trans, useTranslation } from "react-i18next";
import { useLocation, useNavigate } from "react-router";
import type { Me } from "@/api/client";
import { NoticeCard } from "@/components/NoticeCard";
import { BodyText, MutedText, PageTitle } from "@/components/Text";
import { TextLink } from "@/components/TextLink";
import { routes } from "@/lib/routes";

// What silent founding hands Home through router state (R2): the name of the
// Pocket it made. The Household and currency are read live from `me`.
export type FoundedState = { founded: true; pocket: string };

function foundedPocket(state: unknown): string | null {
  const s = state as Partial<FoundedState> | null;
  return s?.founded === true && typeof s.pocket === "string" ? s.pocket : null;
}

// The dashboard (the hero figure and the State) lands with the domain at
// BOOTSTRAP §11 step 9; until then this proves the session. The Shell around it
// carries the app name and sign-out.
export function HomePage({ me }: { me: Me }) {
  const { t } = useTranslation();
  const location = useLocation();
  const navigate = useNavigate();
  // Taken from the history entry once, then the entry is cleared: leaving Home,
  // coming back, or a refresh finds no notice. Nothing is stored (R2).
  const [pocket, setPocket] = useState(() => foundedPocket(location.state));

  useEffect(() => {
    if (foundedPocket(location.state) !== null) {
      void navigate(location.pathname + location.search, { replace: true, state: null });
    }
  }, [location, navigate]);

  return (
    <>
      <PageTitle>{t("home.title")}</PageTitle>
      {pocket !== null && (
        <NoticeCard dismissLabel={t("home.founded.dismiss")} onDismiss={() => setPocket(null)}>
          <BodyText>
            <Trans
              i18nKey="home.founded.named"
              values={{
                household: me.household.household_display_name,
                pocket,
                currency: me.household.reporting_currency,
              }}
              components={{ strong: <strong /> }}
            />
          </BodyText>
          <MutedText>{t("home.founded.hint")}</MutedText>
          <TextLink to={routes.settings}>{t("home.founded.settings")}</TextLink>
        </NoticeCard>
      )}
      <div className="flex flex-col gap-1">
        <BodyText>{t("home.signedInAs", { name: me.user_display_name })}</BodyText>
        <MutedText>{t("home.household", { name: me.household.household_display_name })}</MutedText>
      </div>
      <MutedText>{t("home.comingSoon")}</MutedText>
    </>
  );
}

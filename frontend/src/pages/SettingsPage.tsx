import { useTranslation } from "react-i18next";
import type { ApiError, Me } from "@/api/client";
import { errorMessage } from "@/api/errors";
import { useLogout } from "@/api/queries";
import { HouseholdSection } from "@/components/settings/HouseholdSection";
import { PocketsSection } from "@/components/settings/PocketsSection";
import { YouSection } from "@/components/settings/YouSection";
import { PageTitle } from "@/components/Text";
import { Button } from "@/components/ui/button";
import { toastError } from "@/lib/plainToast";

// One screen in three sections (ui-conventions, Layout and navigation). The
// Pocket dialogs are URL-driven (?new=pocket, ?edit=pocket:<id>) on this path.
// Sign out ends the screen: it is not in the top bar (ruled 27 Nov 2026).
export function SettingsPage({ me }: { me: Me }) {
  const { t } = useTranslation();
  const logout = useLogout();

  function signOut() {
    // No form is open, so a failure is a toast; success unmounts the Shell.
    logout.mutateAsync().catch((e: ApiError) => {
      toastError(errorMessage(t, e));
    });
  }

  return (
    <>
      <PageTitle>{t("settings.title")}</PageTitle>
      <HouseholdSection me={me} />
      <PocketsSection />
      <YouSection me={me} />
      <Button variant="outline" className="w-full" onClick={signOut} disabled={logout.isPending}>
        {t("shell.signOut")}
      </Button>
    </>
  );
}

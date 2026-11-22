import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import type { Me } from "@/api/client";
import { useLogout } from "@/api/queries";
import { errorMessage } from "@/api/errors";
import { Button } from "@/components/ui/button";

// The signed-in shell. The dashboard (the hero figure and the State) lands
// with the domain at BOOTSTRAP §11 step 9; until then this proves the session.
export function HomePage({ me }: { me: Me }) {
  const { t } = useTranslation();
  const logout = useLogout();

  return (
    <main className="mx-auto flex min-h-dvh w-full max-w-md flex-col gap-6 px-4 py-8">
      <header className="flex items-center justify-between gap-4">
        <h1 className="text-xl font-semibold">{t("app.name")}</h1>
        <Button
          variant="outline"
          size="sm"
          onClick={() => logout.mutate()}
          disabled={logout.isPending}
        >
          {t("home.signOut")}
        </Button>
      </header>
      <section className="flex flex-col gap-1">
        <p>{t("home.signedInAs", { name: me.user_display_name })}</p>
        <p className="text-muted-foreground">
          {t("home.household", { name: me.household.household_display_name })}
        </p>
      </section>
      <Link to="/settings/household" className="text-sm underline">
        {t("home.settingsLink")}
      </Link>
      <Link to="/settings/user" className="text-sm underline">
        {t("home.userSettingsLink")}
      </Link>
      <Link to="/pockets" className="text-sm underline">
        {t("home.pocketsLink")}
      </Link>
      <p className="text-muted-foreground">{t("home.comingSoon")}</p>
      {logout.error && (
        <p role="alert" className="text-sm text-destructive">
          {errorMessage(t, logout.error)}
        </p>
      )}
    </main>
  );
}

import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Navigate } from "react-router";
import type { Me } from "@/api/client";
import { useMe } from "@/api/queries";
import { errorMessage } from "@/api/errors";

// Routes behind a session. The /me probe decides: a User renders the child in
// that User's own locale, no session goes to /login, and any other failure is
// shown rather than mistaken for being signed out.
export function RequireSession({ children }: { children: (me: Me) => React.ReactNode }) {
  const { t, i18n } = useTranslation();
  const me = useMe();
  const locale = me.data?.locale;

  useEffect(() => {
    if (locale && i18n.language !== locale) void i18n.changeLanguage(locale);
  }, [locale, i18n]);

  if (me.isPending) {
    return (
      <p role="status" className="p-4 text-muted-foreground">
        {t("app.loading")}
      </p>
    );
  }
  if (me.error) {
    return (
      <p role="alert" className="p-4 text-destructive">
        {errorMessage(t, me.error)}
      </p>
    );
  }
  if (!me.data) return <Navigate to="/login" replace />;
  return <>{children(me.data)}</>;
}

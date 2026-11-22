import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Navigate } from "react-router";
import type { Me } from "@/api/client";
import { useMe } from "@/api/queries";
import { AuthCard } from "@/components/AuthCard";
import { Shell } from "@/components/Shell";
import { ErrorState, Loading } from "@/components/states";
import { routes } from "@/lib/routes";

// Routes behind a session. The /me probe decides: a User renders the child in
// that User's own locale inside the Shell, no session goes to /login, and any
// other failure is shown rather than mistaken for being signed out.
export function RequireSession({ children }: { children: (me: Me) => React.ReactNode }) {
  const { i18n } = useTranslation();
  const me = useMe();
  const locale = me.data?.locale;

  useEffect(() => {
    if (locale && i18n.language !== locale) void i18n.changeLanguage(locale);
  }, [locale, i18n]);

  // Before the Shell: there is no session to put a sign-out button on yet.
  if (me.isPending) {
    return (
      <AuthCard>
        <Loading />
      </AuthCard>
    );
  }
  if (me.error) {
    return (
      <AuthCard>
        <ErrorState error={me.error} onRetry={() => void me.refetch()} />
      </AuthCard>
    );
  }
  if (!me.data) return <Navigate to={routes.login} replace />;
  return <Shell>{children(me.data)}</Shell>;
}

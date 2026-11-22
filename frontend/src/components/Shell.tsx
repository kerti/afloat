import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import { toastError } from "@/lib/plainToast";
import type { ApiError } from "@/api/client";
import { useLogout } from "@/api/queries";
import { errorMessage } from "@/api/errors";
import { Button } from "@/components/ui/button";
import { LogoMark } from "@/components/LogoMark";
import { routes } from "@/lib/routes";

// The frame every signed-in screen renders inside (Uruni web/src/components/
// Shell.tsx, github.com/kerti/uruni). The top bar is fixed, not sticky: a
// sticky header rides iOS's overscroll bounce. Both it and main pad for the
// device's safe areas (index.html carries viewport-fit=cover), and main
// reserves the bar's height: the notch inset, its 3.5rem row and 1px border.
// The one column, max-w-xl, is set here and screens never set their own width.
// There is no bottom tab bar until the dashboard and capture slices decide the
// tabs; when it lands it is fixed too, and main reserves its height as well.
export function Shell({ children }: { children: ReactNode }) {
  const { t } = useTranslation();
  const logout = useLogout();

  function signOut() {
    // No form is open, so a failure is a toast; success unmounts the Shell.
    logout.mutateAsync().catch((e: ApiError) => {
      toastError(errorMessage(t, e));
    });
  }

  return (
    <div className="min-h-dvh">
      <header className="header-glass fixed inset-x-0 top-0 z-10 border-b pt-[env(safe-area-inset-top)]">
        <div className="mx-auto flex h-14 w-full max-w-xl items-center justify-between gap-3 pr-[max(1rem,env(safe-area-inset-right))] pl-[max(1rem,env(safe-area-inset-left))]">
          <div className="flex min-w-0 items-center gap-2.5">
            {/* The mark, left of the name (Uruni Shell.tsx, #356): a second way
                home for a thumb, but not for a keyboard or a screen reader,
                since the name link beside it already is that. */}
            <Link to={routes.home} tabIndex={-1} aria-hidden="true" className="shrink-0">
              <LogoMark className="size-7" />
            </Link>
            <Link
              to={routes.home}
              className="inline-flex min-h-11 items-center rounded-md text-lg font-semibold focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              {t("app.name")}
            </Link>
          </div>
          <Button variant="outline" size="sm" onClick={signOut} disabled={logout.isPending}>
            {t("shell.signOut")}
          </Button>
        </div>
      </header>
      <main className="pt-[calc(env(safe-area-inset-top)+3.5rem+1px+1.5rem)] pr-[max(1rem,env(safe-area-inset-right))] pb-[calc(env(safe-area-inset-bottom)+2rem)] pl-[max(1rem,env(safe-area-inset-left))]">
        <div className="mx-auto flex w-full max-w-xl flex-col gap-6">{children}</div>
      </main>
    </div>
  );
}

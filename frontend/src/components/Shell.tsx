import { useEffect, useRef, type MouseEvent, type ReactNode } from "react";
import { ChartPie, GearSix, House, Plus, Receipt, type Icon } from "@phosphor-icons/react";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Link, NavLink, useLocation, useNavigationType } from "react-router";
import { AppVersion } from "@/components/AppVersion";
import { LogoMark } from "@/components/LogoMark";
import { routes } from "@/lib/routes";

// The frame every signed-in screen renders inside (Uruni web/src/components/
// Shell.tsx, github.com/kerti/uruni). The top bar is fixed, not sticky: a
// sticky header rides iOS's overscroll bounce. Both it and main pad for the
// device's safe areas (index.html carries viewport-fit=cover), and main
// reserves the bar's height: the notch inset, its 3.5rem row and 1px border.
// The one column, max-w-xl, is set here and screens never set their own width.
// The bottom tab bar is fixed too, always visible, and main reserves its height
// as well. Sign out lives at the foot of Settings, not here.
type Tab = { to: string; icon: Icon; label: string; end?: boolean; raised?: boolean };

export function Shell({ children }: { children: ReactNode }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const { pathname, search } = useLocation();
  const navigationType = useNavigationType();
  const tabs: Tab[] = [
    { to: routes.home, icon: House, label: t("shell.tabs.home"), end: true },
    { to: routes.expenses, icon: Receipt, label: t("shell.tabs.expenses") },
    { to: routes.capture, icon: Plus, label: t("shell.tabs.capture"), raised: true },
    { to: routes.budget, icon: ChartPie, label: t("shell.tabs.budget") },
    { to: routes.settings, icon: GearSix, label: t("shell.tabs.settings") },
  ];

  // A new screen opens at its top. Back and forward (POP) leave the scroll
  // alone, and a dialog opening on the same path is not a new screen.
  const shownPath = useRef(pathname);
  useEffect(() => {
    if (shownPath.current === pathname) return;
    shownPath.current = pathname;
    if (navigationType !== "POP") window.scrollTo(0, 0);
  }, [pathname, navigationType]);

  // Tapping the tab already showing reloads what it shows and returns to its
  // top (Uruni). A sub-path or a filter of the tab is not the tab itself: that
  // tap navigates to it as usual.
  function onTab(e: MouseEvent, to: string) {
    if (pathname !== to || search !== "") return;
    e.preventDefault();
    window.scrollTo(0, 0);
    void qc.refetchQueries({ type: "active" });
  }

  return (
    <div className="min-h-dvh">
      <header className="header-glass fixed inset-x-0 top-0 z-10 border-b pt-[env(safe-area-inset-top)]">
        <div className="mx-auto flex h-14 w-full max-w-xl items-center justify-between gap-3 pr-[max(1rem,env(safe-area-inset-right))] pl-[max(1rem,env(safe-area-inset-left))]">
          <div className="flex min-h-11 min-w-0 items-center gap-2.5">
            {/* The mark, left of the name (Uruni Shell.tsx, #356): a second way
                home for a thumb, but not for a keyboard or a screen reader,
                since the name link beside it already is that. */}
            <Link to={routes.home} tabIndex={-1} aria-hidden="true" className="shrink-0">
              <LogoMark className="size-7" />
            </Link>
            {/* The running build under the name, as Uruni's header carries it. */}
            <div className="flex min-w-0 flex-col">
              <Link
                to={routes.home}
                className="truncate rounded-md text-lg leading-6 font-semibold focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                {t("app.name")}
              </Link>
              <AppVersion />
            </div>
          </div>
        </div>
      </header>
      <main className="pt-[calc(env(safe-area-inset-top)+3.5rem+1px+1.5rem)] pr-[max(1rem,env(safe-area-inset-right))] pb-[calc(env(safe-area-inset-bottom)+4rem+2rem)] pl-[max(1rem,env(safe-area-inset-left))]">
        <div className="mx-auto flex w-full max-w-xl flex-col gap-6">{children}</div>
      </main>
      <nav
        aria-label={t("shell.mainNav")}
        className="bar-glass fixed inset-x-0 bottom-0 z-10 border-t pb-[env(safe-area-inset-bottom)]"
      >
        <ul className="mx-auto flex w-full max-w-xl items-stretch">
          {tabs.map(({ to, icon: TabIcon, label, end, raised }) => (
            <li key={to} className="flex-1">
              <NavLink
                to={to}
                end={end}
                onClick={(e) => onTab(e, to)}
                className={({ isActive }) =>
                  `relative flex min-h-15 flex-col items-center justify-end gap-1 px-1 pb-2 text-[11px] leading-4 font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${
                    isActive ? "text-primary" : "text-muted-foreground"
                  }`
                }
              >
                {({ isActive }) => (
                  <>
                    {isActive && (
                      <span
                        aria-hidden="true"
                        className="absolute inset-x-0 -top-px h-0.5 bg-primary"
                      />
                    )}
                    <span className="flex h-6 items-center justify-center">
                      {raised ? (
                        <span className="fab-glass absolute -top-7 left-1/2 flex size-14 -translate-x-1/2 items-center justify-center rounded-2xl bg-primary text-primary-foreground">
                          <TabIcon aria-hidden="true" className="size-7" weight="bold" />
                        </span>
                      ) : (
                        <TabIcon aria-hidden="true" className="size-6" />
                      )}
                    </span>
                    {/* Positioned, so it paints over the raised square's
                        shadow, which otherwise greys Capture's label. */}
                    <span className="relative">{label}</span>
                  </>
                )}
              </NavLink>
            </li>
          ))}
        </ul>
      </nav>
    </div>
  );
}

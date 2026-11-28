import { useTranslation } from "react-i18next";
import { useRegisterSW } from "virtual:pwa-register/react";
import { Button } from "@/components/ui/button";
import { clearOfTabBar } from "@/lib/tabBar";

// The service worker never swaps the app out from under the user
// (registerType: 'prompt' in vite.config.ts): a new version waits until they
// choose to reload. It sits where toasts do, clear of the tab bar and above it
// (the bar is z-10), so both buttons can be tapped (#153).
export function UpdatePrompt() {
  const { t } = useTranslation();
  const {
    needRefresh: [needRefresh, setNeedRefresh],
    updateServiceWorker,
  } = useRegisterSW();

  if (!needRefresh) return null;
  return (
    <div
      role="status"
      style={{ bottom: clearOfTabBar }}
      className="fixed inset-x-4 z-20 mx-auto flex max-w-md items-center justify-between gap-3 rounded-lg border bg-raised p-3 text-card-foreground shadow-lg"
    >
      <span className="text-sm">{t("pwa.updateReady")}</span>
      <span className="flex gap-2">
        <Button size="sm" variant="ghost" onClick={() => setNeedRefresh(false)}>
          {t("pwa.later")}
        </Button>
        <Button size="sm" onClick={() => void updateServiceWorker(true)}>
          {t("pwa.reload")}
        </Button>
      </span>
    </div>
  );
}

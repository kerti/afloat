import { useTranslation } from "react-i18next";
import { useRegisterSW } from "virtual:pwa-register/react";
import { Button } from "@/components/ui/button";

// The service worker never swaps the app out from under the user
// (registerType: 'prompt' in vite.config.ts): a new version waits until they
// choose to reload.
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
      className="fixed inset-x-4 bottom-[calc(1rem+env(safe-area-inset-bottom))] mx-auto flex max-w-md items-center justify-between gap-3 rounded-lg border bg-card p-3 text-card-foreground shadow-lg"
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

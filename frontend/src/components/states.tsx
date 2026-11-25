import { CircleNotch, WarningCircle } from "@phosphor-icons/react";
import { useTranslation } from "react-i18next";
import type { ApiError } from "@/api/client";
import { errorMessage } from "@/api/errors";
import { Button } from "@/components/ui/button";

// Loading is a small inline spinner plus text, never a skeleton; a load failure
// is an alert with a full-width Retry (Uruni web/src/components/states/
// Loading.tsx and ErrorState.tsx, github.com/kerti/uruni).
export function Loading() {
  const { t } = useTranslation();
  return (
    <p role="status" className="flex items-center gap-2 text-muted-foreground">
      <CircleNotch aria-hidden="true" weight="regular" className="size-4 animate-spin" />
      {t("app.loading")}
    </p>
  );
}

export function ErrorState({ error, onRetry }: { error: ApiError; onRetry: () => void }) {
  const { t } = useTranslation();
  return (
    <div role="alert" className="flex flex-col gap-2 text-attention">
      <p className="flex items-center gap-2">
        <WarningCircle aria-hidden="true" className="size-5 shrink-0" />
        {errorMessage(t, error)}
      </p>
      <Button className="w-full" onClick={onRetry}>
        {t("app.retry")}
      </Button>
    </div>
  );
}

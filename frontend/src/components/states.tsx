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
      <svg
        aria-hidden="true"
        viewBox="0 0 24 24"
        className="size-4 animate-spin"
        fill="none"
        stroke="currentColor"
        strokeWidth="3"
        strokeLinecap="round"
      >
        <path d="M12 3a9 9 0 1 0 9 9" />
      </svg>
      {t("app.loading")}
    </p>
  );
}

export function ErrorState({ error, onRetry }: { error: ApiError; onRetry: () => void }) {
  const { t } = useTranslation();
  return (
    <div role="alert" className="flex flex-col gap-2 text-destructive">
      <p className="flex items-center gap-2">
        <svg
          aria-hidden="true"
          viewBox="0 0 24 24"
          className="size-5 shrink-0"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <circle cx="12" cy="12" r="9" />
          <path d="M12 7v6M12 16.5v.5" />
        </svg>
        {errorMessage(t, error)}
      </p>
      <Button className="w-full" onClick={onRetry}>
        {t("app.retry")}
      </Button>
    </div>
  );
}

import type { TFunction } from "i18next";
import type { ApiError } from "./client";

// One place that turns an ApiError into the user's language. Codes, not
// messages, come off the wire (CLAUDE.md rule 7).
export function errorMessage(t: TFunction, err: ApiError): string {
  switch (err.code) {
    case "VALIDATION": {
      const field = typeof err.args.field === "string" ? err.args.field : "";
      return t("errors.VALIDATION", { field: t(`fields.${field}`, { defaultValue: field }) });
    }
    case "TOO_MANY_ATTEMPTS":
      return err.retryAfterSeconds === undefined
        ? t("errors.TOO_MANY_ATTEMPTS_soon")
        : t("errors.TOO_MANY_ATTEMPTS", {
            seconds: err.retryAfterSeconds,
            count: err.retryAfterSeconds,
          });
    default:
      return t(`errors.${err.code}`);
  }
}

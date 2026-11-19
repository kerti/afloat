import type { TFunction } from "i18next";
import type { ApiError } from "./client";

// One place that turns an ApiError into the user's language. Codes, not
// messages, come off the wire (CLAUDE.md rule 7).
export function errorMessage(t: TFunction, err: ApiError): string {
  switch (err.code) {
    case "VALIDATION": {
      const field = typeof err.args.field === "string" ? err.args.field : "";
      // fieldNames, not fields: fields are the form's capitalised labels, and
      // this name sits mid-sentence.
      return t("errors.VALIDATION", { field: t(`fieldNames.${field}`, { defaultValue: field }) });
    }
    case "TOO_MANY_ATTEMPTS":
      // Retry-After: 0 means "now", which no sentence about a wait reads well.
      return err.retryAfterSeconds === undefined || err.retryAfterSeconds === 0
        ? t("errors.TOO_MANY_ATTEMPTS_soon")
        : t("errors.TOO_MANY_ATTEMPTS", { count: err.retryAfterSeconds });
    default:
      return t(`errors.${err.code}`);
  }
}

import type { TFunction } from "i18next";
import type { ApiError } from "./client";

// One place that turns an ApiError into the user's language. Codes, not
// messages, come off the wire (CLAUDE.md rule 7).
export function errorMessage(t: TFunction, err: ApiError): string {
  switch (err.code) {
    case "VALIDATION": {
      // A nested path ("allocations.0.allocation") is named by its last part.
      const field =
        typeof err.args.field === "string" ? err.args.field.replace(/^allocations\.\d+\./, "") : "";
      // fieldNames, not fields: fields are the form's capitalised labels, and
      // this name sits mid-sentence.
      return t("errors.VALIDATION", { field: t(`fieldNames.${field}`, { defaultValue: field }) });
    }
    case "TOO_MANY_ATTEMPTS":
      // Retry-After: 0 means "now", which no sentence about a wait reads well.
      return err.retryAfterSeconds === undefined || err.retryAfterSeconds === 0
        ? t("errors.TOO_MANY_ATTEMPTS_soon")
        : t("errors.TOO_MANY_ATTEMPTS", { count: err.retryAfterSeconds });
    case "WEAK_PASSWORD": {
      // The reason rides in args: `min` or `breached` (contract).
      const reason = err.args.reason === "breached" ? "breached" : "min";
      return t(`errors.WEAK_PASSWORD.${reason}`);
    }
    default:
      return t(`errors.${err.code}`);
  }
}

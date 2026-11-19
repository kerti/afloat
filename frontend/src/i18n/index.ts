import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import enGB from "@/locales/en-GB.json";
import idID from "@/locales/id-ID.json";
import type { components } from "@/api/schema.gen";

export type Locale = components["schemas"]["Locale"];

// The contract's Locale enum, both real from day one (PRD N12a).
export const locales: readonly Locale[] = ["en-GB", "id-ID"];
export const defaultLocale: Locale = "en-GB";

// Before sign-in there is no User locale yet, so the browser's preference
// decides; after it, the User's own setting does (App.tsx).
export function browserLocale(languages: readonly string[] = navigator.languages): Locale {
  for (const lang of languages) {
    const lower = lang.toLowerCase();
    if (lower === "id" || lower.startsWith("id-")) return "id-ID";
    if (lower === "en" || lower.startsWith("en-")) return "en-GB";
  }
  return defaultLocale;
}

export function initI18n(lng: Locale = browserLocale()) {
  void i18n.use(initReactI18next).init({
    resources: {
      "en-GB": { translation: enGB },
      "id-ID": { translation: idID },
    },
    lng,
    fallbackLng: defaultLocale,
    supportedLngs: locales,
    interpolation: { escapeValue: false },
    returnNull: false,
  });
  return i18n;
}

export { i18n };

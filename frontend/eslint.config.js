import js from "@eslint/js";
import globals from "globals";
import reactHooks from "eslint-plugin-react-hooks";
import reactRefresh from "eslint-plugin-react-refresh";
import tseslint from "typescript-eslint";
import { defineConfig, globalIgnores } from "eslint/config";
import prettier from "eslint-config-prettier";

export default defineConfig([
  globalIgnores(["dist", "coverage", "src/api/schema.gen.ts"]),
  {
    files: ["**/*.{ts,tsx}"],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommended,
      reactHooks.configs.flat.recommended,
      reactRefresh.configs.vite,
      prettier,
    ],
    languageOptions: {
      globals: globals.browser,
    },
    rules: {
      // The frontend owns every user-facing string, in en-GB and id-ID both
      // (PRD N12a, CLAUDE.md non-negotiable 7), so bare copy in JSX is an
      // error, not a warning: it would ship English to an id-ID household.
      // eslint-plugin-react is not ESLint 10-compatible, so the rule is
      // expressed as AST selectors, as Balances does.
      "no-restricted-syntax": [
        "error",
        {
          selector: "JSXText[value=/[A-Za-z]/]",
          message: "Bare JSX text - use t('namespace.key').",
        },
        {
          selector:
            "JSXAttribute[name.name=/^(placeholder|title|aria-label|alt)$/] > Literal[value=/\\S/]",
          message: "Bare string attribute - use t('namespace.key').",
        },
      ],
    },
  },
  {
    files: ["src/components/ui/**/*.{ts,tsx}"],
    rules: {
      "react-refresh/only-export-components": "off",
    },
  },
  {
    files: ["**/*.test.{ts,tsx}", "src/test/**"],
    rules: {
      "no-restricted-syntax": "off",
    },
  },
]);

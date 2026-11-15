// Lint du frontend (make lint). Règles recommandées + hooks React ; les tests ont leurs propres globals.
import js from "@eslint/js";
import globals from "globals";
import reactHooks from "eslint-plugin-react-hooks";
import tseslint from "typescript-eslint";

export default tseslint.config(
  { ignores: ["dist", "public", "node_modules", "e2e/test-results", "playwright-report"] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ["**/*.{ts,tsx}"],
    languageOptions: { globals: globals.browser },
    plugins: { "react-hooks": reactHooks },
    rules: {
      ...reactHooks.configs.recommended.rules,
      // `const { secret: _x, ...body } = c` sert à retirer des champs avant l'envoi à l'API.
      "@typescript-eslint/no-unused-vars": ["error", { ignoreRestSiblings: true }],
    },
  },
  { files: ["*.config.{js,ts}", "e2e/**"], languageOptions: { globals: globals.node } },
);

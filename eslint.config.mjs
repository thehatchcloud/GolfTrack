import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";

const eslintConfig = defineConfig([
  ...nextVitals,
  ...nextTs,
  // Override default ignores of eslint-config-next.
  globalIgnores([
    // Default ignores of eslint-config-next:
    ".next/**",
    "out/**",
    "build/**",
    "next-env.d.ts",
    // The PocketBase app is Go; only its gitignored .local/ scratch dir could
    // ever hold JS-ish artifacts (e.g. generated .d.ts), so keep that out.
    "pocketbase/.local/**",
  ]),
]);

export default eslintConfig;

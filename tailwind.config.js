/**
 * Tailwind config for the PocketBase frontend.
 *
 * Scans the Go templates and writes into the embedded static FS the binary
 * serves from.
 *
 * @type {import('tailwindcss').Config}
 */
module.exports = {
  content: ["./pocketbase/internal/web/templates/**/*.html"],
  theme: {
    extend: {},
  },
  plugins: [],
};

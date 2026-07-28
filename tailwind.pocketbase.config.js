/**
 * Tailwind config for the PocketBase frontend (Phase 7B).
 *
 * Separate from tailwind.config.js because both frontends exist at once until
 * the Phase 11 cleanup: that one scans the Django template tree and writes
 * static/css/app.css, this one scans the Go templates and writes into the
 * embedded static FS the binary serves from. Same Tailwind version, so the
 * ported markup keeps rendering identically.
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

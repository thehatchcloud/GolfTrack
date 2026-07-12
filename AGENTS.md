# Rewrite in progress (#85): Django is the deployed app

GolfTrack is migrating to **Django + Django Ninja + Tailwind CSS**. As of Phase 7
(#93) the deployed artifact is the Django app (`Dockerfile` → gunicorn + WhiteNoise
+ Litestream). Read `DJANGO.md` for the Django layout and dev commands, and
`CLAUDE.md` / `DEPLOYMENT.md` for architecture and deploy details. The legacy
Next.js app below remains in-tree until the Phase 8 cutover (#94).

<!-- BEGIN:nextjs-agent-rules -->
# This is NOT the Next.js you know

This version has breaking changes — APIs, conventions, and file structure may all differ from your training data. Read the relevant guide in `node_modules/next/dist/docs/` before writing any code. Heed deprecation notices.
<!-- END:nextjs-agent-rules -->

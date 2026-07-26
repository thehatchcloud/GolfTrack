/// <reference path="../../pb_data/types.d.ts" />

/**
 * GolfTrack hook entrypoint.
 *
 * PocketBase loads every `*.pb.js` file directly inside the hooks directory at
 * startup, in alphabetical order. GolfTrack deliberately keeps exactly one such
 * file: domain logic lives in plain CommonJS modules under `domain/` and shared
 * infrastructure under `lib/`, and this file is the only place that registers
 * hooks. That keeps registration order explicit and reviewable instead of
 * depending on filenames.
 *
 * Two JSVM constraints shape everything under this directory:
 *
 *  1. Handler callbacks execute in a *pooled* runtime that does not share scope
 *     with this file. Anything a handler needs must be `require`d inside the
 *     handler body, not captured from module scope.
 *  2. `require` needs an absolute path; use the `__hooks` global, which holds
 *     this directory.
 *
 * Phase 1 registers no domain behaviour — see `pocketbase/ARCHITECTURE.md` for
 * the modules Phase 3 (#124) adds and the hooks each one binds.
 */

onBootstrap((e) => {
    // Let PocketBase finish booting before touching the app, per the JSVM docs.
    e.next();

    const { NAMES } = require(`${__hooks}/lib/collections.js`);

    // Registered domain modules, in registration order. Phase 3 appends here as
    // each module lands; an empty list means schema-only behaviour.
    const modules = [];

    $app.logger().info(
        "GolfTrack hooks loaded",
        "collections",
        Object.values(NAMES).join(","),
        "domainModules",
        modules.length ? modules.join(",") : "(none — Phase 1)",
    );
});

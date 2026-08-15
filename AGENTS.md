# GolfTrack

GolfTrack runs on **PocketBase** — see `CLAUDE.md` for the project overview and
`POCKETBASE.md` for the architecture. The app lives entirely in `pocketbase/`
(Go); its own `README.md`, `ARCHITECTURE.md`, `API.md`, `AUTH.md`, and
`DEPLOYMENT.md` cover layout, business logic, the API contract, auth, and the
container respectively.

## Cursor Cloud specific instructions

Standard commands live in the `Makefile` and `pocketbase/README.md`; the notes
below only cover the non-obvious parts of running this in the cloud VM.

- **Go toolchain:** the base VM ships Go 1.22, but `go.mod` requires Go 1.25.
  With the default `GOTOOLCHAIN=auto`, any `go` command (`go mod download`,
  `go build`, `make pb-test`) transparently fetches and uses `go1.25.0` — no
  manual Go install is needed. The update script warms this cache.
- **Lint / test / build:** `make pb-test` (`go vet` + full suite, ~40s, all
  in-process, no external services), `make pb-dev` runs the app. Build a
  standalone binary with `cd pocketbase && go build -o .local/golftrack-pb .`.
- **Running the app so you can sign in:** the app has **no sign-in method
  unless you configure one**. There is no OAuth in the cloud VM, so start it
  with password login enabled:
  `GOLFTRACK_ALLOW_PASSWORD_LOGIN=true pocketbase/scripts/dev.sh` (serves on
  `http://127.0.0.1:8090`, Admin UI at `/_/`). `dev.sh` also upserts the
  superuser `dev@golftrack.local` / `devdevdevdev`.
- **Creating an app user:** self-service sign-up is OAuth-only and stays closed
  even with password login on — password login only authenticates accounts that
  already exist. Create a user by POSTing to `/api/collections/users/records`
  with a superuser token (auth via `/api/collections/_superusers/auth-with-password`).
  Set `role` to `ADMIN` if the account needs to create/edit courses (course
  writes are admin-only); include `password`/`passwordConfirm` so it can log in.
- **Data / reset:** all dev state lives in `pocketbase/.local/` (gitignored);
  `make clean` or deleting that directory fully resets the instance. The
  embedded `pb_schema.json` re-syncs on every startup.

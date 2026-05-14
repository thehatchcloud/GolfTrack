# Deployment Guide

This app is ready to deploy as a single-container Next.js application with SQLite.

Recommended deployment model:
- GitHub repo for source control
- Docker-based deploy to your exe.dev server
- persistent disk mount for SQLite database storage

---

## 1. Deployment model

The app uses:
- Next.js standalone output
- Prisma with SQLite
- single process web server
- one SQLite file stored on persistent disk

This is a good fit for a simple private or low-traffic deployment.

---

## 2. Files added for deployment

- `Dockerfile`
- `.dockerignore`
- `.env.example`
- `DEPLOYMENT.md`

---

## 3. Required environment variables

Create a `.env` file or set these in your server runtime:

```env
NODE_ENV=production
PORT=3000
DATABASE_URL="file:/data/prod.db"
```

### Important
`/data/prod.db` should live on a **persistent mounted volume**, not ephemeral container storage.

---

## 4. Build locally

From the app directory:

```bash
cd golf-app
npm install
npm run build
```

To test production mode locally:

```bash
npm start
```

---

## 5. Docker build

Build the image:

```bash
docker build -t golf-track .
```

Run it locally:

```bash
docker run \
  --rm \
  -p 3000:3000 \
  -e DATABASE_URL="file:/data/prod.db" \
  -v $(pwd)/data:/data \
  golf-track
```

Then open:

```text
http://localhost:3000
```

---

## 6. Database setup in production

Because the app uses SQLite, the DB file must be stored on a persistent disk.

Recommended mounted path:
- container path: `/data`
- database file: `/data/prod.db`

### First deploy
Before first production start, run migrations:

```bash
docker run \
  --rm \
  -e DATABASE_URL="file:/data/prod.db" \
  -v /your/persistent/path:/data \
  golf-track \
  npx prisma migrate deploy
```

Optional seed for a non-empty first environment:

```bash
docker run \
  --rm \
  -e DATABASE_URL="file:/data/prod.db" \
  -v /your/persistent/path:/data \
  golf-track \
  npx prisma db seed
```

For real production, you will likely skip seed unless you want demo/sample data.

---

## 7. Suggested deploy flow on exe.dev

Exact steps depend on how exe.dev expects services to be defined, but the general flow is:

1. push code to GitHub
2. connect the repo to exe.dev
3. configure Docker build using `Dockerfile`
4. attach a persistent volume mounted at `/data`
5. set env vars:
   - `NODE_ENV=production`
   - `PORT=3000`
   - `DATABASE_URL=file:/data/prod.db`
6. run `npx prisma migrate deploy` before or during first release
7. start the app

If exe.dev supports a release command, use:

```bash
npx prisma migrate deploy
```

If it only supports a start command, run migrations manually before first boot.

---

## 8. GitHub setup recommendation

Recommended repo contents:
- application source
- prisma schema and migrations
- deployment files
- spec/docs if you want to keep them in repo

Do **not** commit:
- `.env`
- local sqlite db files
- test db files
- `node_modules`
- `.next`

You can commit:
- `prisma/migrations`
- `prisma/schema.prisma`

---

## 9. Backup recommendation

Since production uses SQLite, backup is simple.

### Minimum recommendation
Regularly copy the database file:

```bash
cp /mounted/data/prod.db /mounted/backups/prod-$(date +%F-%H%M%S).db
```

### Better recommendation
- daily backup job
- retain several days/weeks
- optionally copy backup off-server

---

## 10. Upgrade / deploy updates

For code changes:
1. push updated code to GitHub
2. rebuild image on exe.dev
3. run migrations:
   ```bash
   npx prisma migrate deploy
   ```
4. roll out new container

Because the DB is on a persistent volume, the data remains across deploys.

---

## 11. Operational notes

### This setup is good for
- personal use
- private family/friends use
- low concurrency
- simple maintenance

### This setup is not ideal for
- high write concurrency
- multi-instance horizontal scaling
- large public production traffic

If the app grows significantly, the next step would likely be moving from SQLite to Postgres.

---

## 12. Quick start checklist

- [ ] push repo to GitHub
- [ ] configure exe.dev app from repo
- [ ] mount persistent volume to `/data`
- [ ] set `DATABASE_URL=file:/data/prod.db`
- [ ] run `npx prisma migrate deploy`
- [ ] deploy app
- [ ] verify home page loads
- [ ] create a course
- [ ] start and submit a round
- [ ] confirm DB persists after restart

---

## 13. Commands reference

### Local dev
```bash
npm run dev
```

### Local tests
```bash
npm test
```

### Local production build
```bash
npm run build
npm start
```

### Prisma local migration
```bash
npm run db:migrate
```

### Prisma production migration
```bash
npm run db:deploy
```

---

## 14. Recommendation for your setup

Given your plan:
- host code in GitHub
- deploy to exe.dev server

I recommend:
- use the included `Dockerfile`
- attach persistent storage at `/data`
- set `DATABASE_URL=file:/data/prod.db`
- run `npx prisma migrate deploy` on each deploy

That is the simplest clean production path for this app.

# Golf Track

A mobile-first golf score tracking web app built with:
- Next.js
- TypeScript
- Tailwind CSS
- Prisma
- SQLite

## Features

- create 9-hole and 18-hole courses
- define par for each hole
- start rounds from saved courses
- for 18-hole courses, choose:
  - full course
  - front 9
  - back 9
- track shots hole-by-hole
- record the club used for each shot
- undo, edit, and delete shots
- review and submit rounds with notes
- cancel in-progress rounds
- browse completed round history

## Local development

```bash
npm install
npm run db:migrate
npm run dev
```

Open:

```text
http://localhost:3000
```

## Database

Local development uses SQLite.

Default env:

```env
DATABASE_URL="file:./dev.db"
```

## Tests

```bash
npm test
```

## Production / deployment

See:
- `DEPLOYMENT.md`
- `.env.example`
- `Dockerfile`

Recommended production setup:
- Docker container
- persistent volume mounted at `/data`
- `DATABASE_URL=file:/data/prod.db`

## Prisma commands

```bash
npm run db:migrate
npm run db:deploy
npm run db:seed
```

---
title: The railway alias
description: The `openrail` binary also answers to `railway`, so scripts and habits built on Railway's CLI keep working.
---

The `openrail` binary also answers to `railway`, so scripts and habits built on Railway's CLI keep working.

## How it works

`openrail alias install` adds a `railway` symlink next to the `openrail` binary. It leaves a real `railway` binary alone if one is already on your `PATH`. When the binary runs as `railway`, it accepts Railway's verbs and flags, for example:

```sh
railway login
railway link
railway up --service api
railway variables --set "FOO=bar"
railway add -d postgres
railway logs
railway run -- npm run migrate
```

The first time, it prints one line: `Using openrail at <url>; run openrail alias remove to undo`.

A verb openrail does not have answers with `railway <verb> is not available in openrail. See openrail.app/docs/coming-from-railway` and exits non-zero.

## Manage the alias

```sh
openrail alias install
openrail alias remove
```

## Tokens

As `railway`, the CLI also reads `RAILWAY_TOKEN` and `RAILWAY_API_TOKEN`. Use an openrail API token as the value, not a Railway token.

## Next steps

- [Coming from Railway](/docs/coming-from-railway)
- [CLI overview](/docs/cli/overview)

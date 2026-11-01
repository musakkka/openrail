---
title: Templates and the Deploy on openrail button
description: Deploy ready-made apps such as n8n, Umami or Uptime Kuma in one click, and add a Deploy on openrail button to your own README.
---

Deploy ready-made apps such as n8n, Umami or Uptime Kuma in one click, and add a Deploy on openrail button to your own README.

## Deploy a template

1. On the canvas, select **+ Add**, then **Template**.
2. Pick a template. The detail page shows the services it creates and its inputs; generated values (secrets) are prefilled and can be regenerated.
3. Fill in required inputs and select **Deploy**.

Templates deploy their services in dependency order, for example Postgres before n8n. From the CLI: `openrail deploy --template n8n`.

The list is in [Templates reference](/docs/reference/templates).

## Deep links

`https://<your console>/new/template/<id>?KEY=value` opens a template with inputs prefilled.

## The README button

Add this to a README:

```md
[![Deploy on openrail](https://openrail.app/button.svg)](https://openrail.app/deploy?template=umami)
```

The button opens `openrail.app/deploy`, which asks for the visitor's console URL once, remembers it in their browser, and sends them to their own console's template page. Extra query parameters are passed through as inputs. Add `&change=1` to pick a different console.

## Writing a template

See [Writing a template](/docs/contribute/writing-a-template).

---
title: Config file schema
description: The fields `railway.json`, `railway.toml`, `openrail.yaml` and `openrail.json` accept, with what each one sets.
---

The fields `railway.json`, `railway.toml`, `openrail.yaml` and `openrail.json` accept, with what each one sets.

## Files

The first of `openrail.yaml`, `openrail.json`, `railway.json`, `railway.toml` in the root directory is used, or the file named in **Settings > Source > Config file path**.

## Fields

| Field | Sets | Service setting |
|---|---|---|
| `build.builder` | `RAILPACK`, `DOCKERFILE` or `NIXPACKS` (built with Railpack) | `build.builder` |
| `build.buildCommand` | Build command | `build.buildCommand` |
| `build.dockerfilePath` | Dockerfile path | `build.dockerfilePath` |
| `build.watchPatterns` | Watch paths | `build.watchPaths` |
| `build.railpackVersion` | Railpack version | `build.railpackVersion` |
| `deploy.startCommand` | Start command | `deploy.startCommand` |
| `deploy.preDeployCommand` | Pre-deploy command | `deploy.preDeployCommand` |
| `deploy.healthcheckPath` | Health check path | `deploy.healthcheckPath` |
| `deploy.healthcheckTimeout` | Health check timeout in seconds | `deploy.healthcheckTimeoutSec` |
| `deploy.restartPolicyType` | `ON_FAILURE`, `ALWAYS` or `NEVER` | `deploy.restartPolicy` |
| `deploy.restartPolicyMaxRetries` | Maximum restarts | `deploy.restartMaxRetries` |
| `deploy.cronSchedule` | Cron schedule | `deploy.cronSchedule` |
| `deploy.numReplicas` | Replicas | `deploy.replicas` |
| `deploy.multiRegionConfig` | Replica count only (sum of the regions) | `deploy.replicas` |
| `deploy.overlapSeconds` | Overlap seconds | `deploy.overlapSec` |
| `deploy.drainingSeconds` | Draining seconds | `deploy.drainingSec` |
| `deploy.sleepApplication` | Ignored with a notice | |
| `deploy.processes` (openrail files only) | Processes, or `auto` | `deploy.processes` |
| `deploy.placement` (openrail files only) | `{roles, hosts}` | `deploy.placement` |
| `deploy.singleton` (openrail files only) | Singleton | `deploy.singleton` |
| `environments.<name>` | The same fields, for one environment | |

## Example `openrail.yaml`

```yaml
build:
  builder: RAILPACK
  watchPatterns:
    - /api/**
deploy:
  startCommand: node server.js
  healthcheckPath: /health
  processes: auto
environments:
  production:
    deploy:
      numReplicas: 2
```

YAML keys are camelCase, as in JSON.

## Rules

- Priority: the current environment's block, then the top-level fields, then the console settings.
- Unknown fields fail the build with the line number and a suggestion.
- Fields set by the file are locked in the console and marked with a file icon.

## Next steps

- [Config as code](/docs/guides/config-as-code)

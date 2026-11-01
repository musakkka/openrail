#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# Runs `openrail up` for the "Deploy with openrail" action (action.yml at the
# repository root). Inputs arrive as INPUT_* variables, the instance and
# token as OPENRAIL_URL and OPENRAIL_TOKEN. Writes the outputs url and
# deployment-id to $GITHUB_OUTPUT. Exits with the CLI's exit code.

set -euo pipefail

err() { printf '::error::%s\n' "$*" >&2; exit 1; }
truthy() { case "$1" in true | True | TRUE | yes | 1 | on) return 0 ;; esac; return 1; }

[ -n "${OPENRAIL_TOKEN:-}" ] || err "The token input is empty. Store an environment token as a secret and pass it as token."
[ -n "${OPENRAIL_URL:-}" ] || err "The url input is empty. Pass the console URL of your openrail install."
case "$OPENRAIL_URL" in http://* | https://*) ;; *) err "url must start with https:// (got $OPENRAIL_URL)." ;; esac
printf '::add-mask::%s\n' "$OPENRAIL_TOKEN"

# Keep the CLI's config out of the runner's home directory.
tmp=${RUNNER_TEMP:-$(mktemp -d)}
export OPENRAIL_CONFIG="$tmp/openrail-cli/config.json"
mkdir -p "$(dirname "$OPENRAIL_CONFIG")"

bin=${FIFTY_BIN:-openrail}
args=(up --yes)
[ -n "${INPUT_PROJECT:-}" ] && args+=(--project "$INPUT_PROJECT")
[ -n "${INPUT_ENVIRONMENT:-}" ] && args+=(--environment "$INPUT_ENVIRONMENT")
[ -n "${INPUT_SERVICE:-}" ] && args+=(--service "$INPUT_SERVICE")

detach=""
if truthy "${INPUT_DETACH:-false}"; then
	detach=1
	args+=(--detach)
else
	args+=(--ci)
fi
if truthy "${INPUT_NEW_SERVICE:-false}"; then
	args+=(--new)
	[ -n "${INPUT_SERVICE_NAME:-}" ] && args+=(--name "$INPUT_SERVICE_NAME")
elif [ -n "${INPUT_SERVICE_NAME:-}" ]; then
	err "service-name only works together with new-service: true."
fi
truthy "${INPUT_PATH_AS_ROOT:-false}" && args+=(--path-as-root)

message=${INPUT_MESSAGE:-}
if [ -z "$message" ]; then
	sha=${GITHUB_SHA:-}
	subject=$(git log -1 --format=%s 2>/dev/null || true)
	if [ -n "$subject" ]; then
		message="$subject (${sha:0:7})"
	elif [ -n "$sha" ]; then
		message="${GITHUB_REPOSITORY:-GitHub Actions}@${sha:0:7}"
	fi
fi
[ -n "$message" ] && args+=(--message "$message")

if [ -n "${INPUT_ARGS:-}" ]; then
	read -r -a extra <<<"$INPUT_ARGS"
	[ "${#extra[@]}" -gt 0 ] && args+=("${extra[@]}")
fi
[ -n "${INPUT_PATH:-}" ] && args+=("$INPUT_PATH")

log="$tmp/openrail-up.log"
printf 'openrail'
printf ' %q' "${args[@]}"
printf '\n'
set +e
"$bin" "${args[@]}" 2>&1 | tee "$log"
code=${PIPESTATUS[0]}
set -e

out=${GITHUB_OUTPUT:-/dev/null}
url=$(sed -n 's/^Deployed: \(https\{0,1\}:\/\/[^ ]*\).*/\1/p' "$log" | tail -n 1)
did=""
if [ -n "$detach" ]; then
	did=$(sed -n 's/^Deployment \([^ ]*\) started\..*/\1/p' "$log" | tail -n 1)
fi
{
	echo "url=$url"
	echo "deployment-id=$did"
} >>"$out"

if [ "$code" -ne 0 ]; then
	printf '::error::openrail up failed with exit code %s. The log above has the diagnosis.\n' "$code" >&2
	exit "$code"
fi
if [ -n "$url" ] && [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
	echo "Deployed: $url" >>"$GITHUB_STEP_SUMMARY"
fi

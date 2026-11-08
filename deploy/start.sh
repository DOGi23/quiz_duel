#!/bin/sh
set -eu
cd "$(dirname "$0")"
if [ -z "${BOT_TOKEN:-}" ]; then
    if [ ! -r .secrets/bot-token ]; then
        echo 'Pass BOT_TOKEN or provide a protected .secrets/bot-token file.' >&2
        exit 1
    fi
    BOT_TOKEN=$(cat .secrets/bot-token)
fi
export BOT_TOKEN
exec docker compose --project-name quiz-duel -f compose.production.yaml up -d --no-build --pull never

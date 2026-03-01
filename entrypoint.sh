#!/bin/sh
set -e

if [ -d /migrations ] && [ -n "$DATABASE_URL" ]; then
    echo "Running database migrations..."
    migrate -path /migrations -database "$DATABASE_URL" up
    echo "Migrations complete."
fi

exec "$@"

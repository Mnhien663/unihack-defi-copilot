#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

operation="${1:-}"
database_name="${2:-deficopilot_db}"

case "$database_name" in
  deficopilot_db|deficopilot_migration_test)
    ;;
  *)
    echo "Unsupported database name." >&2
    exit 1
    ;;
esac

case "$operation" in
  up|version)
    migration_args=("$operation")
    ;;
  down)
    if [[ "$database_name" != "deficopilot_migration_test" ]]; then
      echo "Down is allowed only on deficopilot_migration_test." >&2
      exit 1
    fi

    migration_args=("down" "1")
    ;;
  *)
    echo "Usage: bash scripts/migrate.sh {up|version|down} [database]" >&2
    exit 1
    ;;
esac

MSYS_NO_PATHCONV=1 docker compose run --rm \
  --entrypoint migrate \
  migrate \
  -path /migrations \
  -database "postgres://admin@db:5432/${database_name}?sslmode=disable" \
  "${migration_args[@]}"
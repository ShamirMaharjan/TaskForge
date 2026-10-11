#!/usr/bin/env bash
set -e

# Fail loudly if project-specific password variables are missing
: "${TASKFORGE_APP_DB_PASS:?Environment variable TASKFORGE_APP_DB_PASS must be set and non-empty}"
: "${TASKFORGE_CRON_DB_PASS:?Environment variable TASKFORGE_CRON_DB_PASS must be set and non-empty}"

psql -U postgres -v app_pass="${TASKFORGE_APP_DB_PASS}" -v cron_pass="${TASKFORGE_CRON_DB_PASS}" <<'EOF'
  SELECT format('CREATE ROLE taskforge_app WITH LOGIN PASSWORD %L', :'app_pass')
  WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'taskforge_app');
  \gexec

  SELECT format('CREATE ROLE taskforge_cron WITH LOGIN PASSWORD %L', :'cron_pass')
  WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'taskforge_cron');
  \gexec
EOF
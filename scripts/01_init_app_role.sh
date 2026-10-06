#!/usr/bin/env bash
psql -U postgres -c "CREATE ROLE flowmind_app WITH LOGIN PASSWORD '${APP_DB_PASS}';"
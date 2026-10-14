# Development Notes

## Ticket creation errors

Missing configured defaults such as the `medium` priority are infrastructure
errors and should preserve the underlying database error (`pgx.ErrNoRows`).
Invalid caller input uses the exported ticket-domain sentinels so callers can
match errors with `errors.Is`.

## Integration tests

Set `TASKFORGE_REQUIRE_INTEGRATION=1` in CI to turn missing integration
database credentials into test failures instead of skips.

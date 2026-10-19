# Development Notes

## Ticket creation errors

Missing configured defaults such as the `medium` priority are infrastructure
errors and should preserve the underlying database error (`pgx.ErrNoRows`).
Invalid caller input uses the exported ticket-domain sentinels so callers can
match errors with `errors.Is`.

## Integration tests

Set `TASKFORGE_REQUIRE_INTEGRATION=1` in CI to turn missing integration
database credentials into test failures instead of skips.

## Assign design

### Actor rule

An actor may assign a ticket when the actor row has `status = 'active'` and
either the actor's role slug is `admin`, or the actor's role slug is
`department_admin` and the actor's `department_id` equals the ticket's
`tag_department_id`. Pending, rejected, and deactivated actors are never
allowed, regardless of role.

### Transitions

Assignment is allowed from `unassigned` to `assigned`. Reassignment of an
`assigned` or `in_progress` ticket keeps the current status and changes only
the assignee. `resolved` and `cancel` are terminal for assignment and must
reject the operation.

### Target validation

`ValidateAssignee` should load the target user and role slug inside the
transaction. The target must be active and have an assignable role. Read
validation alone is not sufficient: lock the target row with `FOR UPDATE` so
deactivation cannot commit between validation and the ticket update.

### Errors and HTTP mapping

The service should expose domain errors for ticket-not-found (`404`), actor
not allowed (`403`), invalid transition (`409`), and invalid assignee (`422`).
The update must use a guarded `UPDATE` predicate and check its affected-row
count. A zero-row update after the ticket was loaded means a concurrent
change won the race, so it should surface as a conflict rather than being
reported as an invalid input.

### Audit entry

The audit `old_value` and `new_value` should both contain the assignee ID and
status. The old value records the ticket state before assignment; the new
value records the resulting state. Both values should be derived from the
locked ticket row and the committed update.

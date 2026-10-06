-- Read-only permissions for fixed lookup tables
GRANT SELECT ON roles, statuses TO flowmind_app;

-- Full edit permissions for admin-managed lookups
GRANT SELECT, INSERT, UPDATE ON departments, categories, priorities TO flowmind_app;

-- Application tables (Read, Insert, Update — NO DELETE)
GRANT SELECT, INSERT, UPDATE ON users, user_passwords, oauth_accounts, refresh_tokens, email_verification_tokens TO flowmind_app;
GRANT SELECT, INSERT, UPDATE ON tickets, comments TO flowmind_app;

-- Audit table (Append-Only: Read and Insert only)
GRANT SELECT, INSERT ON audit TO flowmind_app;
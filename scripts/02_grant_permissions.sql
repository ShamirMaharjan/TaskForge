-- Code-Owned Lookups (Read-Only)
GRANT SELECT ON roles, statuses TO taskforge_app;

-- Admin-Editable Lookups (INSERT allowed; UPDATE strictly restricted to column 'name')
GRANT SELECT, INSERT ON departments, categories, priorities TO taskforge_app;
GRANT UPDATE (name) ON departments, categories, priorities TO taskforge_app;

-- Operational Application Tables (No DELETE granted to taskforge_app)
GRANT SELECT, INSERT, UPDATE ON users, user_passwords, oauth_accounts, email_verification_tokens, refresh_tokens TO taskforge_app;
GRANT SELECT, INSERT, UPDATE ON tickets, comments TO taskforge_app;

-- Append-Only Audit Table
GRANT SELECT, INSERT ON audit TO taskforge_app;
REVOKE UPDATE, DELETE, TRUNCATE ON audit FROM taskforge_app;

-- Maintenance Cron Permissions (Strictly restricted to session and token table cleanup)
GRANT SELECT, DELETE ON refresh_tokens, email_verification_tokens TO taskforge_cron;
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username VARCHAR(100) NOT NULL,
    email VARCHAR(255) NOT NULL,
    full_name VARCHAR(200) NOT NULL,
    department_id UUID NOT NULL REFERENCES departments(id),
    role_id UUID NOT NULL REFERENCES roles(id),
    status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'active', 'rejected', 'deactivated')),
    email_verified BOOLEAN NOT NULL DEFAULT FALSE,
    is_system BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX uq_users_username_lower ON users (LOWER(username));
CREATE UNIQUE INDEX uq_users_email_lower ON users (LOWER(email));

CREATE TRIGGER trg_users_updated_at
BEFORE UPDATE ON users
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Seed System Automation User
INSERT INTO users (
    id,
    username,
    email,
    full_name,
    department_id,
    role_id,
    status,
    email_verified,
    is_system
) VALUES (
    '00000000-0000-0000-0000-000000000000',
    'system',
    'system@taskforge.local',
    'System Automation',
    (SELECT id FROM departments WHERE name = 'IT' LIMIT 1),
    (SELECT id FROM roles WHERE slug = 'admin' LIMIT 1),
    'active',
    TRUE,
    TRUE
);

CREATE TABLE user_passwords (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    password_hash VARCHAR(255) NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TRIGGER trg_user_passwords_updated_at
BEFORE UPDATE ON user_passwords
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE oauth_accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    issuer VARCHAR(255) NOT NULL,
    provider_user_id VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_oauth_issuer_sub UNIQUE (issuer, provider_user_id)
);

CREATE INDEX idx_oauth_accounts_user_id ON oauth_accounts(user_id);

-- Stores pending registration details until verification link submission
CREATE TABLE email_verification_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email VARCHAR(255) NOT NULL,
    requested_username VARCHAR(100) NOT NULL,
    department_id UUID NOT NULL REFERENCES departments(id),
    token_hash VARCHAR(255) NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_email_verification_tokens_email ON email_verification_tokens (LOWER(email));

CREATE TABLE refresh_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash VARCHAR(255) NOT NULL UNIQUE,
    replaced_by_token_id UUID NULL REFERENCES refresh_tokens(id) ON DELETE RESTRICT,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_refresh_tokens_user_id ON refresh_tokens(user_id);
CREATE OR REPLACE FUNCTION prevent_ticket_tampering()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.reporter_id IS DISTINCT FROM OLD.reporter_id 
       OR NEW.request IS DISTINCT FROM OLD.request THEN
        RAISE EXCEPTION 'ticket request description and reporter_id are immutable.';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TABLE tickets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request VARCHAR(500) NOT NULL,
    reporter_id UUID NOT NULL REFERENCES users(id),
    assignee_id UUID NULL REFERENCES users(id),
    tag_department_id UUID NOT NULL REFERENCES departments(id),
    status_id UUID NOT NULL REFERENCES statuses(id),
    category_id UUID NOT NULL REFERENCES categories(id),
    priority_id UUID NOT NULL REFERENCES priorities(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at TIMESTAMPTZ NULL,
    first_response_at TIMESTAMPTZ NULL
);

CREATE INDEX idx_tickets_reporter_id ON tickets(reporter_id);
CREATE INDEX idx_tickets_assignee_id ON tickets(assignee_id);
CREATE INDEX idx_tickets_status_id ON tickets(status_id);
CREATE INDEX idx_tickets_tag_department_id ON tickets(tag_department_id);

CREATE TRIGGER trg_tickets_updated_at
BEFORE UPDATE ON tickets
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER trg_tickets_immutable
BEFORE UPDATE ON tickets
FOR EACH ROW EXECUTE FUNCTION prevent_ticket_tampering();

CREATE TABLE comments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    content VARCHAR(2000) NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id),
    ticket_id UUID NOT NULL REFERENCES tickets(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_comments_ticket_id ON comments(ticket_id);

CREATE TRIGGER trg_comments_updated_at
BEFORE UPDATE ON comments
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE audit (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id UUID NOT NULL REFERENCES users(id),
    action VARCHAR(100) NOT NULL,
    entity_type VARCHAR(50) NOT NULL,
    entity_id UUID NOT NULL,
    ticket_id UUID NULL REFERENCES tickets(id),
    old_value JSONB NULL,
    new_value JSONB NULL,
    logged_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_ticket_id ON audit(ticket_id);
CREATE INDEX idx_audit_entity ON audit(entity_type, entity_id);

CREATE OR REPLACE FUNCTION block_audit_tampering()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'Audit log table is append-only.';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_audit_immutable_row
BEFORE UPDATE OR DELETE ON audit
FOR EACH ROW EXECUTE FUNCTION block_audit_tampering();

CREATE TRIGGER trg_audit_immutable_stmt
BEFORE TRUNCATE ON audit
FOR EACH STATEMENT EXECUTE FUNCTION block_audit_tampering();
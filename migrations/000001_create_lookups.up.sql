CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION prevent_slug_tampering()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.slug IS DISTINCT FROM OLD.slug THEN
        RAISE EXCEPTION 'Table slug values are immutable once created.';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TABLE departments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL UNIQUE
);

CREATE TABLE roles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug VARCHAR(50) NOT NULL UNIQUE,
    name VARCHAR(100) NOT NULL UNIQUE
);

CREATE TRIGGER trg_roles_immutable_slug
BEFORE UPDATE ON roles
FOR EACH ROW EXECUTE FUNCTION prevent_slug_tampering();

CREATE TABLE statuses (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug VARCHAR(50) NOT NULL UNIQUE,
    name VARCHAR(100) NOT NULL UNIQUE,
    lifecycle_stage VARCHAR(20) NOT NULL CHECK (lifecycle_stage IN ('OPEN', 'CLOSED', 'CANCELLED'))
);

CREATE TRIGGER trg_statuses_immutable_slug
BEFORE UPDATE ON statuses
FOR EACH ROW EXECUTE FUNCTION prevent_slug_tampering();

CREATE TABLE categories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug VARCHAR(50) NOT NULL UNIQUE,
    name VARCHAR(100) NOT NULL UNIQUE
);

CREATE TRIGGER trg_categories_immutable_slug
BEFORE UPDATE ON categories
FOR EACH ROW EXECUTE FUNCTION prevent_slug_tampering();

CREATE TABLE priorities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug VARCHAR(50) NOT NULL UNIQUE,
    name VARCHAR(100) NOT NULL UNIQUE,
    weight INTEGER NOT NULL UNIQUE
);

CREATE TRIGGER trg_priorities_immutable_slug
BEFORE UPDATE ON priorities
FOR EACH ROW EXECUTE FUNCTION prevent_slug_tampering();

INSERT INTO roles (slug, name) VALUES
    ('admin', 'Admin'),
    ('department_admin', 'Department Admin'),
    ('employee', 'Employee');

INSERT INTO statuses (slug, name, lifecycle_stage) VALUES
    ('unassigned', 'Unassigned', 'OPEN'),
    ('assigned', 'Assigned', 'OPEN'),
    ('in_progress', 'In Progress', 'OPEN'),
    ('resolved', 'Resolved', 'CLOSED'),
    ('cancel', 'Cancelled', 'CANCELLED');

INSERT INTO priorities (slug, name, weight) VALUES
    ('low', 'Low', 10),
    ('medium', 'Medium', 20),
    ('high', 'High', 30),
    ('urgent', 'Urgent', 40);

INSERT INTO categories (slug, name) VALUES 
    ('general', 'General'), 
    ('hardware', 'Hardware'), 
    ('software', 'Software'), 
    ('network', 'Network');

INSERT INTO departments (name) VALUES 
    ('IT'), 
    ('HR'), 
    ('Finance'), 
    ('Operations');
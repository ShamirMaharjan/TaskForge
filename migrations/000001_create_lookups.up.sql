CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN 
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TABLE departments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(50) NOT NULL UNIQUE
);

CREATE TABLE roles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug VARCHAR(50) NOT NULL UNIQUE,
    name VARCHAR(100) NOT NULL UNIQUE
);

CREATE TABLE statuses (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug VARCHAR(50) NOT NULL UNIQUE,
    name VARCHAR(100) NOT NULL UNIQUE,
    lifecycle_stage VARCHAR(20) NOT NULL CHECK (lifecycle_stage IN ('OPEN', 'CLOSED', 'CANCELLED'))
);

CREATE TABLE categories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug VARCHAR(50) NOT NULL UNIQUE,
    name VARCHAR(100) NOT NULL UNIQUE
);

CREATE TABLE priorities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug VARCHAR(50) NOT NULL UNIQUE,
    name VARCHAR(100) NOT NULL UNIQUE,
    weight INTEGER NOT NULL UNIQUE 
);

-- Initial System Seeds

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

INSERT INTO priorities (slug,name, weight) VALUES
('urgent','Urgent', 40),
('high','High', 30),
('medium','Medium', 20),
('low','Low', 10);

INSERT INTO categories (slug,name) VALUES 
('general','General'), 
('hardware','Hardware'), 
('software','Software'), 
('network','Network');

INSERT INTO departments (name) VALUES 
('IT'), 
('HR'), 
('Finance'), 
('Operations');


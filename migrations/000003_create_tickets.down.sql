DROP TRIGGER IF EXISTS trg_audit_immutable_stmt ON audit;
DROP TRIGGER IF EXISTS trg_audit_immutable_row ON audit;
DROP FUNCTION IF EXISTS block_audit_tampering();
DROP TABLE IF EXISTS audit;
DROP TABLE IF EXISTS comments;
DROP TRIGGER IF EXISTS trg_tickets_immutable ON tickets;
DROP FUNCTION IF EXISTS prevent_ticket_tampering();
DROP TABLE IF EXISTS tickets;
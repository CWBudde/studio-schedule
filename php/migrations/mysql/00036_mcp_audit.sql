-- The audit action of the MCP server (ADR 0022): a tool an assistant called
-- as the account of `mcp --as`, recorded for every call with the tool and
-- the outcome in the details, never the arguments. Conventions as in
-- 00001_core.sql.

-- +goose Up

INSERT INTO audit_actions (action) VALUES ('mcp-tool-called');

-- +goose Down

DELETE FROM audit_events WHERE action = 'mcp-tool-called';

DELETE FROM audit_actions WHERE action = 'mcp-tool-called';

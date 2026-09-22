-- +goose Up
CREATE TRIGGER chat_audit_event_immutable BEFORE UPDATE OR DELETE ON chat_audit_event FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();

-- +goose Down
DROP TRIGGER chat_audit_event_immutable ON chat_audit_event;

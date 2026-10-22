-- REV-102-03 follow-up: agent_owner_binding (00721) and brand_asset_revision
-- (00364) are sealed by a forbid_mutation trigger, yet both migrations granted
-- UPDATE to the runtime role. A sealed table must not advertise a privilege the
-- trigger will always refuse, so the runtime role keeps SELECT and INSERT only.
-- +goose Up
REVOKE UPDATE ON agent_owner_binding, brand_asset_revision FROM hcmnext_app;
-- +goose Down
GRANT UPDATE ON agent_owner_binding, brand_asset_revision TO hcmnext_app;

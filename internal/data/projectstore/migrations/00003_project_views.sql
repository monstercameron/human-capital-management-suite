-- +goose Up
CREATE TABLE project_board_view (
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    id text NOT NULL,
    audience text NOT NULL CHECK (audience IN ('PERSONAL','PROJECT')),
    owner_id text NOT NULL DEFAULT '',
    name text NOT NULL DEFAULT '',
    swimlane_value_order jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(swimlane_value_order) = 'array'),
    revision bigint NOT NULL CHECK (revision > 0),
    config_json jsonb NOT NULL CHECK (jsonb_typeof(config_json) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, project_id, id),
    FOREIGN KEY (tenant_id, project_id) REFERENCES project(tenant_id, id),
    CHECK ((audience = 'PROJECT' AND owner_id = '') OR (audience = 'PERSONAL' AND owner_id <> ''))
);
CREATE INDEX project_board_view_audience ON project_board_view(tenant_id, project_id, audience, owner_id, id);

-- +goose StatementBegin
ALTER TABLE project_board_view ENABLE ROW LEVEL SECURITY;
ALTER TABLE project_board_view FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON project_board_view
  USING (tenant_id = current_setting('hcmnext.tenant_id', true))
  WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));
-- +goose StatementEnd

-- +goose Down
DROP TABLE project_board_view;

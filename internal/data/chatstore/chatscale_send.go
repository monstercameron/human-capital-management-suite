package chatstore

// The initial post and its immutable revision belong to the same transaction.
// Writing both in one statement avoids a round trip inside the conversation's
// sequence fence. Rendering preparation still runs after the revision exists.
const chatscaleInsertPostSQL = `WITH posted AS (
 INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,parent_id,references_json,source_attribution,client_key,created_at,updated_at)
 VALUES($1,$2,$3,$4,$5,$11,$6,$7,$8,$9,$10,COALESCE($12,now()),COALESCE($12,now()))
 RETURNING id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,revision,tombstoned,created_at,updated_at,parent_id,references_json,source_attribution
), recorded AS (
 INSERT INTO chat_post_revision(tenant_id,post_id,revision,author_id,body)
 SELECT tenant_id,id,revision,author_id,body FROM posted RETURNING 1
)
 SELECT id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,revision,tombstoned,created_at,updated_at,parent_id,references_json,source_attribution FROM posted`

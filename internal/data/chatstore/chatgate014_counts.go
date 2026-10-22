package chatstore

// CHATGATE-014: a post restricted to some members is not counted for anybody
// else. The sidebar's unread and mention counts take their posts from
// chatscaleEligible and chatscaleCandidates, so the audience rule is written into
// those two and a count can never include what the reader cannot open.
//
// The rule is the one in chatgate_audience.go (the author and the members frozen
// with the post read it); this copy also names the conversation, which the
// counting queries always know, so the lookup uses chat_post_audience's
// restricted-post index (tenant, conversation) and a channel without restricted
// posts costs an empty probe. $3 is the reader's subject, as in the counts.
const chatgateCountAudience = ` AND NOT EXISTS (SELECT 1 FROM chat_post_audience au WHERE au.tenant_id=p.tenant_id AND au.conversation_id=p.conversation_id AND au.post_id=p.id AND au.restricted AND au.author_id<>$3 AND NOT ($3=ANY(au.member_ids)))`

// chatgateRootAudience5 is the same rule for the thread-follow lookup, whose
// reader is its fifth parameter.
const chatgateRootAudience5 = ` AND NOT EXISTS (SELECT 1 FROM chat_post_audience au WHERE au.tenant_id=p.tenant_id AND au.conversation_id=p.conversation_id AND au.post_id=p.id AND au.restricted AND au.author_id<>$5 AND NOT ($5=ANY(au.member_ids)))`

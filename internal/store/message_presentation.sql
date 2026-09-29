-- Display-only evidence. Old records remain NULL; never reconstruct reasoning.
ALTER TABLE messages ADD COLUMN presentation TEXT CHECK(presentation IS NULL OR
 (role='assistant' AND json_valid(presentation) AND length(CAST(presentation AS BLOB))<=65536));
DROP TRIGGER message_immutable;
CREATE TRIGGER message_immutable BEFORE UPDATE ON messages
 WHEN OLD.retired_by IS NOT NULL OR NEW.retired_by IS NULL
 OR NEW.id IS NOT OLD.id OR NEW.session_id IS NOT OLD.session_id OR NEW.turn_id IS NOT OLD.turn_id
 OR NEW.group_id IS NOT OLD.group_id OR NEW.sequence IS NOT OLD.sequence OR NEW.role IS NOT OLD.role
 OR NEW.opening_input IS NOT OLD.opening_input OR NEW.source_session_id IS NOT OLD.source_session_id
 OR NEW.source_message_id IS NOT OLD.source_message_id OR NEW.source_sequence IS NOT OLD.source_sequence
 OR NEW.presentation IS NOT OLD.presentation
 OR NEW.design_context IS NOT OLD.design_context
 OR NEW.input_id IS NOT OLD.input_id OR NEW.parts IS NOT OLD.parts OR NEW.created_at IS NOT OLD.created_at
 OR NEW.model_continuation IS NOT OLD.model_continuation OR NEW.mail_id IS NOT OLD.mail_id
 OR NEW.mail_revision IS NOT OLD.mail_revision OR NEW.mail_presentation IS NOT OLD.mail_presentation
 OR NOT EXISTS(SELECT 1 FROM history_edits e JOIN sessions s ON s.id=e.session_id WHERE e.id=NEW.retired_by AND e.session_id=NEW.session_id
  AND e.revision=NEW.retired_revision AND s.history_revision=e.revision AND NEW.sequence>e.keep_through AND NEW.sequence<=e.observed_through)
 BEGIN SELECT RAISE(ABORT, 'transcript entry only permits exact retirement'); END;

-- Forks retain failed display evidence with their imported canonical group,
-- without manufacturing local attempts, turns, usage or execution authority.
ALTER TABLE history_groups ADD COLUMN attempt_presentations TEXT CHECK(attempt_presentations IS NULL OR
 (source_session_id IS NOT NULL AND json_valid(attempt_presentations) AND json_type(attempt_presentations)='array'
 AND json_array_length(attempt_presentations)<=64 AND length(CAST(attempt_presentations AS BLOB))<=4194304));

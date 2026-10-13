-- Media view: list attachments by channel scope with keyset paging on
-- uploader/size/name as well as time. Attachment ids are ULIDs, so the
-- primary key already serves the time order.
CREATE INDEX IF NOT EXISTS idx_attach_uploader ON attachments(uploader_id, id);
CREATE INDEX IF NOT EXISTS idx_playlist_items_upload ON playlist_items(upload_attachment_id);

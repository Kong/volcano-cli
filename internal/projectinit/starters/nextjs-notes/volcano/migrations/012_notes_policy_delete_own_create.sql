CREATE POLICY notes_delete_own
    ON notes FOR DELETE
    USING (user_id = auth.uid());

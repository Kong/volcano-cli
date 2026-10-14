CREATE POLICY notes_update_own
    ON notes FOR UPDATE
    USING (user_id = auth.uid())
    WITH CHECK (user_id = auth.uid());

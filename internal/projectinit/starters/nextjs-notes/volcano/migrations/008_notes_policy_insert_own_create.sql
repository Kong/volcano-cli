CREATE POLICY notes_insert_own
    ON notes FOR INSERT
    WITH CHECK (user_id = auth.uid());

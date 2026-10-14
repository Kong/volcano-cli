CREATE POLICY notes_select_own
    ON notes FOR SELECT
    USING (user_id = auth.uid());

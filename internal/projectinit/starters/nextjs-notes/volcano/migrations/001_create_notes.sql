-- Notes table for nextjs-demo quickstart
CREATE TABLE IF NOT EXISTS notes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL DEFAULT auth.uid(),
    title TEXT NOT NULL,
    content TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS notes_user_id_idx ON notes(user_id);
CREATE INDEX IF NOT EXISTS notes_created_at_idx ON notes(created_at DESC);

ALTER TABLE notes ENABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS notes_select_own ON notes;
CREATE POLICY notes_select_own
    ON notes FOR SELECT
    USING (user_id = auth.uid());

DROP POLICY IF EXISTS notes_insert_own ON notes;
CREATE POLICY notes_insert_own
    ON notes FOR INSERT
    WITH CHECK (user_id = auth.uid());

DROP POLICY IF EXISTS notes_update_own ON notes;
CREATE POLICY notes_update_own
    ON notes FOR UPDATE
    USING (user_id = auth.uid())
    WITH CHECK (user_id = auth.uid());

DROP POLICY IF EXISTS notes_delete_own ON notes;
CREATE POLICY notes_delete_own
    ON notes FOR DELETE
    USING (user_id = auth.uid());

CREATE OR REPLACE FUNCTION update_notes_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_notes_updated_at ON notes;
CREATE TRIGGER trg_notes_updated_at
    BEFORE UPDATE ON notes
    FOR EACH ROW
    EXECUTE FUNCTION update_notes_updated_at();

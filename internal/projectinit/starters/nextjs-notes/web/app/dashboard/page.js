"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { getVolcanoClient } from "../../lib/volcano";

export default function DashboardPage() {
  const router = useRouter();
  const [user, setUser] = useState(null);
  const [notes, setNotes] = useState([]);
  const [title, setTitle] = useState("");
  const [content, setContent] = useState("");
  const [summary, setSummary] = useState(null);
  const [message, setMessage] = useState("");
  const [loading, setLoading] = useState(true);
  const [deletingNoteID, setDeletingNoteID] = useState("");

  useEffect(() => {
    async function initializeDashboard() {
      const volcano = getVolcanoClient();
      const session = await volcano.initialize();

      if (!session.user) {
        router.push("/");
        return;
      }

      setUser(session.user);
      await loadNotes(volcano);
      setLoading(false);
    }

    initializeDashboard();
  }, [router]);

  async function loadNotes(volcano) {
    const result = await volcano
      .from("notes")
      .select("id,title,content,created_at")
      .order("created_at", { ascending: false });

    if (result.error) {
      setMessage(result.error.message || "Failed to load notes");
      setNotes([]);
      return;
    }

    setNotes(result.data || []);
    setMessage("");
  }

  async function handleCreateNote(event) {
    event.preventDefault();

    if (!title.trim()) {
      setMessage("Title is required.");
      return;
    }

    const volcano = getVolcanoClient();
    const result = await volcano.insert("notes", {
      title: title.trim(),
      content: content.trim()
    });

    if (result.error) {
      setMessage(result.error.message || "Failed to create note");
      return;
    }

    setTitle("");
    setContent("");
    await loadNotes(volcano);
  }

  async function handleSignOut() {
    const volcano = getVolcanoClient();
    await volcano.auth.signOut();
    router.push("/");
  }

  function normalizeFunctionResponse(resultData) {
    const maybeBody = resultData?.body;

    if (typeof maybeBody === "string") {
      try {
        return JSON.parse(maybeBody);
      } catch (_err) {
        return { body: maybeBody };
      }
    }

    return resultData;
  }

  async function handleDeleteNote(noteId) {
    if (!noteId || deletingNoteID === noteId) {
      return;
    }

    if (!window.confirm("Delete this note?")) {
      return;
    }

    setDeletingNoteID(noteId);

    try {
      const volcano = getVolcanoClient();
      const result = await volcano.delete("notes").eq("id", noteId);

      if (result.error) {
        setMessage(result.error.message || "Failed to delete note");
        return;
      }

      await loadNotes(volcano);
    } catch (err) {
      setMessage(err?.message || "Failed to delete note");
    } finally {
      setDeletingNoteID("");
    }
  }

  async function handleInvokeSummary() {
    const volcano = getVolcanoClient();
    const result = await volcano.functions.invoke("notes-summary", { limit: 5 });

    if (result.error) {
      setMessage(result.error.message || "Failed to invoke notes-summary");
      return;
    }

    setSummary(normalizeFunctionResponse(result.data));
    setMessage("");
  }

  if (loading) {
    return (
      <section className="panel">
        <p className="muted">Loading dashboard...</p>
      </section>
    );
  }

  return (
    <section className="panel">
      <div className="row row-between header-row">
        <h1>Dashboard</h1>
        <div className="row account-actions">
          <div className="account-chip" title={user?.email || "unknown"}>{user?.email || "unknown"}</div>
          <button type="button" className="ghost-button" onClick={handleSignOut}>Sign out</button>
        </div>
      </div>

      <form onSubmit={handleCreateNote} className="stack">
        <label>
          Title
          <input type="text" value={title} onChange={(event) => setTitle(event.target.value)} required />
        </label>

        <label>
          Content
          <textarea value={content} onChange={(event) => setContent(event.target.value)} rows={4} />
        </label>

        <div className="row row-between form-actions">
          <button type="button" className="ghost-button" onClick={handleInvokeSummary}>Invoke notes-summary</button>
          <button type="submit">Save note</button>
        </div>
      </form>

      {summary ? <pre>{JSON.stringify(summary, null, 2)}</pre> : null}
      {message ? <p className="error">{message}</p> : null}

      <div className="row row-between notes-header">
        <h2>Your Notes</h2>
        <p className="muted notes-count">{notes.length} {notes.length === 1 ? "note" : "notes"}</p>
      </div>
      {notes.length === 0 ? <p className="muted">No notes yet.</p> : null}
      <ul className="note-list">
        {notes.map((note) => (
          <li key={note.id} className="note-item">
            <div className="row row-between note-item-header">
              <strong>{note.title}</strong>
              <button type="button" className="ghost-button danger-button" onClick={() => handleDeleteNote(note.id)} disabled={deletingNoteID === note.id}>
                {deletingNoteID === note.id ? "Deleting..." : "Delete"}
              </button>
            </div>
            <p>{note.content}</p>
          </li>
        ))}
      </ul>
    </section>
  );
}

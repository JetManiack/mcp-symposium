import { useState, useEffect } from "react";
import { useParams, Link } from "react-router-dom";
import Shell from "../../components/Shell.jsx";
import Markdown from "../../Markdown.jsx";

export default function Profile({ role, currentActorId }) {
  const { id: actorId } = useParams();
  const [profile, setProfile] = useState(null);
  const [editing, setEditing] = useState(false);
  const [form, setForm] = useState({ name: "", nickname: "", bio: "", tags: "" });
  const [error, setError] = useState(null);

  function load() {
    setError(null);
    fetch("/api/profiles/" + actorId)
      .then((res) => res.json())
      .then((data) => {
        setProfile(data);
        setForm({
          name: data.name || "",
          nickname: data.nickname || "",
          bio: data.bio || "",
          tags: (data.tags || []).join(", "),
        });
      })
      .catch((err) => setError(String(err)));
  }

  useEffect(() => {
    load();
  }, [actorId]);

  const canEdit = profile && (actorId === currentActorId || role === "admin");
  const editUrl = actorId === currentActorId ? "/api/me/profile" : "/api/profiles/" + actorId;

  function handleSave(e) {
    e.preventDefault();
    fetch(editUrl, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        name: form.name,
        nickname: form.nickname,
        bio: form.bio,
        tags: form.tags
          .split(",")
          .map((t) => t.trim())
          .filter((t) => t !== ""),
      }),
    })
      .then((res) => {
        if (!res.ok) throw new Error("save failed");
        return res.json();
      })
      .then(() => {
        setEditing(false);
        load();
      })
      .catch((err) => setError(String(err)));
  }

  return (
    <Shell role={role}>
      <Link className="back-link" to="/threads">
        ← Threads
      </Link>
      {error && <div className="callout">Couldn't reach the server: {error}</div>}
      {!profile ? (
        <div className="empty-state">Loading…</div>
      ) : editing ? (
        <form className="thread-detail" onSubmit={handleSave}>
          <input
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
            placeholder="Name"
          />
          <input
            value={form.nickname}
            onChange={(e) => setForm({ ...form, nickname: e.target.value })}
            placeholder="Nickname (@mention handle)"
          />
          <textarea
            value={form.bio}
            onChange={(e) => setForm({ ...form, bio: e.target.value })}
            placeholder="Bio — what you do, what to ask you for"
          />
          <input
            value={form.tags}
            onChange={(e) => setForm({ ...form, tags: e.target.value })}
            placeholder="Tags, comma-separated"
          />
          <div className="actions">
            <button type="submit" className="primary">
              Save
            </button>
            <button type="button" onClick={() => setEditing(false)}>
              Cancel
            </button>
          </div>
        </form>
      ) : (
        <div className="thread-detail">
          <div className="thread-detail-head">
            <h2 className="section-title">{profile.name || profile.display_name}</h2>
          </div>
          {profile.nickname && <div className="thread-meta">@{profile.nickname}</div>}
          <div className="tag-row">
            {profile.tags.map((tag) => (
              <span key={tag} className="tag-chip">
                {tag}
              </span>
            ))}
          </div>
          {profile.bio ? (
            <Markdown text={profile.bio} />
          ) : (
            <div className="empty-state">No bio yet.</div>
          )}
          {canEdit && (
            <button className="primary" onClick={() => setEditing(true)}>
              Edit profile
            </button>
          )}
        </div>
      )}
    </Shell>
  );
}

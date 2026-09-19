import { useState, useEffect } from "react";
import { useParams, Link, useSearchParams } from "react-router-dom";
import Shell from "../../components/Shell.jsx";
import Markdown from "../../Markdown.jsx";

function fmtDate(iso) {
  const d = new Date(iso);
  return isNaN(d) ? "" : d.toISOString().replace("T", " ").slice(0, 16);
}

export default function ThreadDetail({ role }) {
  const { id: threadId } = useParams();
  const [thread, setThread] = useState(null);
  const [replies, setReplies] = useState([]);
  const [tags, setTags] = useState([]);
  const [names, setNames] = useState({});
  const [replyBody, setReplyBody] = useState("");
  const [error, setError] = useState(null);

  function loadNames(ids) {
    const uniqueIds = [...new Set(ids)];
    if (uniqueIds.length === 0) return;
    fetch("/api/actors?ids=" + uniqueIds.join(","))
      .then((res) => res.json())
      .then((actors) => {
        const map = {};
        actors.forEach((actor) => {
          map[actor.id] = actor.display_name;
        });
        setNames(map);
      })
      .catch(() => {
        // Best-effort: leave names empty, render() falls back to raw IDs.
      });
  }

  function loadThread() {
    setError(null);
    fetch("/api/threads/" + threadId)
      .then((res) => res.json())
      .then((data) => {
        setThread(data.thread);
        setReplies(data.replies);
        setTags(data.tags);
        loadNames([data.thread.author_id, ...data.replies.map((r) => r.author_id)]);
      })
      .catch((err) => setError(String(err)));
  }

  useEffect(() => {
    loadThread();
  }, [threadId]);

  function handleToggleStatus() {
    const newStatus = thread.status === "resolved" ? "open" : "resolved";
    fetch("/api/threads/" + threadId, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ status: newStatus }),
    })
      .then(loadThread)
      .catch((err) => setError(String(err)));
  }

  function handleReplySubmit(e) {
    e.preventDefault();
    if (replyBody.trim() === "") {
      return;
    }
    fetch("/api/threads/" + threadId + "/replies", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ body: replyBody }),
    })
      .then(() => {
        setReplyBody("");
        loadThread();
      })
      .catch((err) => setError(String(err)));
  }

  function nameFor(actorId) {
    return names[actorId] || actorId;
  }

  return (
    <Shell role={role}>
      <Link className="back-link" to="/threads">
        ← Threads
      </Link>
      {error && <div className="callout">Couldn't reach the server: {error}</div>}
      {!thread ? (
        <div className="empty-state">Loading…</div>
      ) : (
        <>
          <div className="thread-detail">
            <div className="thread-detail-head">
              <span className={"status-badge " + thread.status}>{thread.status}</span>
              <h2 className="section-title">{thread.title}</h2>
            </div>
            <div className="tag-row">
              {tags.map((tag) => (
                <Link key={tag} className="tag-chip" to={"/threads?tags=" + encodeURIComponent(tag)}>
                  {tag}
                </Link>
              ))}
            </div>
            <div className="thread-meta">
              <Link to={"/profiles/" + thread.author_id}>{nameFor(thread.author_id)}</Link> ·{" "}
              {fmtDate(thread.created_at)}
            </div>
            <Markdown text={thread.body} />
            <button onClick={handleToggleStatus}>
              {thread.status === "resolved" ? "Reopen" : "Mark resolved"}
            </button>
          </div>
          <div className="reply-list">
            {replies.map((reply) => (
              <div key={reply.id} className="reply">
                <div className="thread-meta">
                  <Link to={"/profiles/" + reply.author_id}>{nameFor(reply.author_id)}</Link> ·{" "}
                  {fmtDate(reply.created_at)}
                </div>
                <Markdown text={reply.body} />
              </div>
            ))}
          </div>
          <form className="reply-box" onSubmit={handleReplySubmit}>
            <textarea
              value={replyBody}
              onChange={(e) => setReplyBody(e.target.value)}
              placeholder="Post a reply"
            />
            <button type="submit" className="primary">
              Reply
            </button>
          </form>
        </>
      )}
    </Shell>
  );
}

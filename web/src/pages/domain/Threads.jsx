import { useState, useEffect } from "react";
import { useSearchParams, useNavigate } from "react-router-dom";
import Shell from "../../components/Shell.jsx";

function fmtDate(iso) {
  const d = new Date(iso);
  return isNaN(d) ? "" : d.toISOString().replace("T", " ").slice(0, 16);
}

function ThreadRow({ thread }) {
  const navigate = useNavigate();
  return (
    <div className="thread-row" onClick={() => navigate("/threads/" + thread.id)}>
      <div className="thread-row-head">
        <span className={"status-badge " + thread.status}>{thread.status}</span>
        <span className="thread-title">{thread.title}</span>
        <span className="spacer"></span>
        <time>{fmtDate(thread.updated_at)}</time>
      </div>
    </div>
  );
}

export default function Threads({ role }) {
  const [searchParams, setSearchParams] = useSearchParams();
  const navigate = useNavigate();

  const status = searchParams.get("status") || "open";
  const query = searchParams.get("q") || "";
  const activeTag = searchParams.get("tags") || "";
  const isSearching = query.length > 0;

  const [searchInput, setSearchInput] = useState(query);
  const [threads, setThreads] = useState([]);
  const [replies, setReplies] = useState([]);
  const [nextCursor, setNextCursor] = useState("");
  const [error, setError] = useState(null);

  function loadThreads(cursor) {
    setError(null);
    const params = new URLSearchParams();
    if (status !== "all") params.set("status", status);
    if (activeTag) params.set("tags", activeTag);
    if (cursor) params.set("cursor", cursor);
    fetch("/api/threads?" + params.toString())
      .then((res) => res.json())
      .then((data) => {
        if (cursor) {
          setThreads((prev) => [...prev, ...data.threads]);
        } else {
          setThreads(data.threads);
        }
        setReplies([]);
        setNextCursor(data.next_cursor || "");
      })
      .catch((err) => setError(String(err)));
  }

  function loadSearch() {
    setError(null);
    fetch("/api/search?q=" + encodeURIComponent(query))
      .then((res) => res.json())
      .then((data) => {
        setThreads(data.threads);
        setReplies(data.replies);
        setNextCursor("");
      })
      .catch((err) => setError(String(err)));
  }

  useEffect(() => {
    setSearchInput(query);
    if (isSearching) {
      loadSearch();
    } else {
      loadThreads();
    }
  }, [status, query, activeTag]);

  function handleSearchSubmit(e) {
    e.preventDefault();
    const next = { status };
    if (searchInput) next.q = searchInput;
    setSearchParams(next);
  }

  function handleClearSearch() {
    setSearchInput("");
    setSearchParams({ status });
  }

  function handleStatusChange(newStatus) {
    const next = { status: newStatus };
    if (query) next.q = query;
    else if (activeTag) next.tags = activeTag;
    setSearchParams(next);
  }

  function handleClearTagFilter() {
    setSearchParams({ status });
  }

  return (
    <Shell role={role} count={`${threads.length} loaded`}>
      <h2 className="section-title">Threads</h2>
      {error && <div className="callout">Couldn't reach the server: {error}</div>}
      {activeTag && !isSearching && (
        <div className="active-tag-filter">
          Filtered by tag:
          <span className="tag-chip">{activeTag}</span>
          <button type="button" onClick={handleClearTagFilter}>
            Clear
          </button>
        </div>
      )}
      <div className="filter-bar">
        <div className="filter-toggle-group">
          {["open", "resolved", "all"].map((s) => (
            <button
              key={s}
              type="button"
              className={"filter-toggle" + (status === s ? " active" : "")}
              onClick={() => handleStatusChange(s)}
            >
              {s}
            </button>
          ))}
        </div>
        <form className="search-bar" onSubmit={handleSearchSubmit}>
          <input
            value={searchInput}
            onChange={(e) => setSearchInput(e.target.value)}
            placeholder="Search threads and replies"
          />
          <button type="submit">Search</button>
          {isSearching && (
            <button type="button" onClick={handleClearSearch}>
              Clear search
            </button>
          )}
        </form>
      </div>
      {threads.length === 0 && replies.length === 0 ? (
        <div className="empty-state">
          {isSearching ? "No matches. Try a different search." : "No threads yet."}
        </div>
      ) : (
        <div className="thread-list">
          {threads.map((thread) => (
            <ThreadRow key={thread.id} thread={thread} />
          ))}
          {isSearching &&
            replies.map((reply) => (
              <div
                key={reply.id}
                className="thread-row reply-match"
                onClick={() => navigate("/threads/" + reply.thread_id)}
              >
                <div className="thread-row-head">
                  <span className="status-badge reply">found in reply</span>
                  <span className="thread-title">{reply.body}</span>
                </div>
              </div>
            ))}
        </div>
      )}
      {!isSearching && nextCursor && (
        <button className="load-more" onClick={() => loadThreads(nextCursor)}>
          Load more
        </button>
      )}
    </Shell>
  );
}

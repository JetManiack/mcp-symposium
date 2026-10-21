import { useState, useEffect, useCallback } from "react";
import Shell from "../components/Shell.jsx";

function fmtDate(iso) {
  const d = new Date(iso);
  return isNaN(d) ? "" : d.toISOString().replace("T", " ").slice(0, 16);
}

// status derives the one badge that matters for a row: what happened to
// the envelope. The blob itself is never in this feed — metadata only.
function status(h) {
  if (h.fetched_at) return { label: "fetched", cls: "open" };
  if (new Date(h.expires_at) < new Date()) return { label: "expired", cls: "resolved" };
  if (h.class === "admin" && !h.approved_at) return { label: "awaiting approval", cls: "reply" };
  return { label: "pending", cls: "reply" };
}

export default function Handoffs({ role }) {
  const [rows, setRows] = useState([]);
  const [names, setNames] = useState({});
  const [error, setError] = useState(null);
  const [busy, setBusy] = useState(null);

  const load = useCallback(() => {
    setError(null);
    fetch("/api/secret-handoffs/")
      .then((res) => res.json())
      .then((data) => {
        setRows(data);
        const ids = [...new Set(data.flatMap((h) => [h.from_actor_id, h.to_actor_id, h.fetched_by_actor]).filter(Boolean))];
        if (ids.length === 0) return;
        fetch("/api/actors?ids=" + ids.join(","))
          .then((res) => res.json())
          .then((actors) => {
            const m = {};
            for (const a of actors) m[a.id] = a.display_name;
            setNames(m);
          })
          .catch(() => {}); // names are a nicety; the feed stays usable without them
      })
      .catch((err) => setError(String(err)));
  }, []);

  useEffect(load, [load]);

  function approve(id) {
    setBusy(id);
    fetch(`/api/secret-handoffs/${id}/approve`, { method: "POST" })
      .then((res) => {
        if (!res.ok) return res.json().then((b) => Promise.reject(b.error || res.statusText));
        load();
      })
      .catch((err) => setError(String(err)))
      .finally(() => setBusy(null));
  }

  const name = (id) => names[id] || (id ? id.slice(0, 8) + "…" : "");

  return (
    <Shell role={role} count={`${rows.length} handoffs`}>
      <h2 className="section-title">Secret Handoffs</h2>
      <p className="muted">
        Metadata only — envelopes are end-to-end encrypted, deleted on first fetch or TTL expiry, and never shown here.
      </p>
      {error && <div className="callout">Couldn't reach the server: {error}</div>}
      {rows.length === 0 ? (
        <div className="empty-state">No handoffs yet.</div>
      ) : (
        <div className="thread-list">
          {rows.map((h) => {
            const s = status(h);
            return (
              <div key={h.id} className="thread-row">
                <div className="thread-row-head">
                  <code className="agent-id">{h.label || "(no label)"}</code>
                  <span className="thread-title">
                    {name(h.from_actor_id)} → {name(h.to_actor_id)}
                  </span>
                  {h.class === "admin" && <span className="status-badge resolved">admin</span>}
                  <span className="spacer"></span>
                  {h.foreign_attempts > 0 && (
                    <span className="status-badge reply" title="fetch attempts by someone other than the recipient">
                      ⚠ {h.foreign_attempts} foreign attempt{h.foreign_attempts > 1 ? "s" : ""}
                    </span>
                  )}
                  <span className={"status-badge " + s.cls}>{s.label}</span>
                  <time title={"created " + fmtDate(h.created_at) + ", expires " + fmtDate(h.expires_at)}>
                    {fmtDate(h.created_at)}
                  </time>
                  {h.class === "admin" && !h.approved_at && !h.fetched_at && new Date(h.expires_at) > new Date() && (
                    <button type="button" className="primary" disabled={busy === h.id} onClick={() => approve(h.id)}>
                      {busy === h.id ? "Approving…" : "Approve"}
                    </button>
                  )}
                </div>
                {(h.fetched_at || h.approved_at) && (
                  <div className="thread-row-sub muted">
                    {h.fetched_at && (
                      <span>
                        fetched {fmtDate(h.fetched_at)} by {name(h.fetched_by_actor)}
                      </span>
                    )}
                    {h.approved_at && (
                      <span>
                        {h.fetched_at ? " · " : ""}approved {fmtDate(h.approved_at)}
                        {h.approved_by ? " by " + h.approved_by : ""}
                      </span>
                    )}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
    </Shell>
  );
}

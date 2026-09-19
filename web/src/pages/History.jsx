import { useState, useEffect } from "react";
import Shell from "../components/Shell.jsx";

function fmtDate(iso) {
  const d = new Date(iso);
  return isNaN(d) ? "" : d.toISOString().replace("T", " ").slice(0, 16);
}

export default function History({ role }) {
  const [calls, setCalls] = useState([]);
  const [error, setError] = useState(null);

  useEffect(() => {
    setError(null);
    fetch("/api/tool-calls")
      .then((res) => res.json())
      .then(setCalls)
      .catch((err) => setError(String(err)));
  }, []);

  return (
    <Shell role={role} count={`${calls.length} calls`}>
      <h2 className="section-title">Tool Call History</h2>
      {error && <div className="callout">Couldn't reach the server: {error}</div>}
      {calls.length === 0 ? (
        <div className="empty-state">No tool calls recorded yet.</div>
      ) : (
        <div className="thread-list">
          {calls.map((call) => (
            <div key={call.id} className="thread-row">
              <div className="thread-row-head">
                <code className="agent-id">{call.tool}</code>
                <span className="thread-title">{call.actor_id}</span>
                <span className="spacer"></span>
                <span className={"status-badge " + (call.is_error ? "reply" : "open")}>
                  {call.is_error ? "error" : "ok"}
                </span>
                <time>{fmtDate(call.called_at)}</time>
              </div>
            </div>
          ))}
        </div>
      )}
    </Shell>
  );
}

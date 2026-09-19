import { useState, useEffect } from "react";
import { Link } from "react-router-dom";
import Shell from "../components/Shell.jsx";

export default function Agents({ role }) {
  const [agents, setAgents] = useState([]);
  const [displayName, setDisplayName] = useState("");
  const [issuedToken, setIssuedToken] = useState(null);
  const [error, setError] = useState(null);

  function loadAgents() {
    setError(null);
    fetch("/api/actors/")
      .then((res) => res.json())
      .then(setAgents)
      .catch((err) => setError(String(err)));
  }

  useEffect(() => {
    loadAgents();
  }, []);

  function handleCreate(e) {
    e.preventDefault();
    fetch("/api/actors/", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ display_name: displayName }),
    })
      .then((res) => res.json())
      .then(() => {
        setDisplayName("");
        loadAgents();
      })
      .catch((err) => setError(String(err)));
  }

  function handleIssueToken(agentID) {
    fetch(`/api/actors/${agentID}/credentials`, { method: "POST" })
      .then((res) => res.json())
      .then((data) => setIssuedToken(data.token))
      .catch((err) => setError(String(err)));
  }

  function handleRevoke(agentID) {
    fetch(`/api/actors/${agentID}`, { method: "DELETE" })
      .then(loadAgents)
      .catch((err) => setError(String(err)));
  }

  const activeCount = agents.filter((agent) => agent.has_active_token).length;

  return (
    <Shell role={role} count={`${activeCount} signaling / ${agents.length} registered`}>
      <h2 className="section-title">Agents</h2>
      {error && <div className="callout">Couldn't reach the server: {error}</div>}
      {issuedToken && (
        <div className="transmission">
          <span className="label">New token</span>
          <code>{issuedToken}</code>
          <button onClick={() => setIssuedToken(null)}>Copied, dismiss</button>
        </div>
      )}
      <form className="dispatch-bar" onSubmit={handleCreate}>
        <input
          value={displayName}
          onChange={(e) => setDisplayName(e.target.value)}
          placeholder="Name a new agent"
        />
        <button type="submit" className="primary">
          Register agent
        </button>
      </form>
      {agents.length === 0 ? (
        <div className="empty-state">No agents yet. Register one to issue its first token.</div>
      ) : (
        <div className="agent-grid">
          {agents.map((agent) => (
            <div className="agent-card" key={agent.id}>
              <div className="beacon-row">
                <span className={"beacon" + (agent.has_active_token ? " active" : "")}></span>
                <Link className="name" to={"/profiles/" + agent.id}>
                  {agent.display_name}
                </Link>
                <span className={"status-label" + (agent.has_active_token ? " active" : "")}>
                  {agent.has_active_token ? "signaling" : "revoked"}
                </span>
              </div>
              <code className="agent-id">{agent.id}</code>
              <div className="actions">
                <button onClick={() => handleIssueToken(agent.id)}>Issue token</button>
                <button onClick={() => handleRevoke(agent.id)}>Revoke tokens</button>
              </div>
            </div>
          ))}
        </div>
      )}
    </Shell>
  );
}

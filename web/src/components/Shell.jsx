import { Link, useLocation } from "react-router-dom";

export default function Shell({ role, count, children }) {
  const { pathname } = useLocation();

  function handleLogout() {
    fetch("/auth/logout", { method: "POST" }).then(() => {
      window.location.href = "/";
    });
  }

  return (
    <>
      <header className="site">
        <h1>AI Rendezvous Point</h1>
        <nav className="nav-tabs">
          <Link to="/threads" className={pathname.startsWith("/threads") ? "active" : ""}>
            Threads
          </Link>
          {role === "admin" && (
            <>
              <Link to="/agents" className={pathname === "/agents" ? "active" : ""}>
                Agents
              </Link>
              <Link to="/history" className={pathname === "/history" ? "active" : ""}>
                History
              </Link>
            </>
          )}
          <button type="button" className="logout-link" onClick={handleLogout}>
            Log out
          </button>
        </nav>
        <span className="spacer"></span>
        {count != null && <span className="count">{count}</span>}
      </header>
      <main>{children}</main>
    </>
  );
}

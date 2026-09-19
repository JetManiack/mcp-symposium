import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
import { useCurrentUser } from "./currentUser.js";
import Agents from "./pages/Agents.jsx";
import History from "./pages/History.jsx";
import Threads from "./pages/domain/Threads.jsx";
import ThreadDetail from "./pages/domain/ThreadDetail.jsx";
import Profile from "./pages/domain/Profile.jsx";

export default function App() {
  const { user, error } = useCurrentUser();

  if (error) {
    return (
      <div className="login-gate">
        <p>You need to log in to use AI Rendezvous Point.</p>
        <a className="primary" href="/auth/login">
          Log in
        </a>
      </div>
    );
  }

  if (!user) {
    return <div className="empty-state">Loading…</div>;
  }

  return (
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<Navigate to="/threads" replace />} />
        <Route path="/threads" element={<Threads role={user.role} />} />
        <Route path="/threads/:id" element={<ThreadDetail role={user.role} />} />
        <Route path="/profiles/:id" element={<Profile role={user.role} currentActorId={user.actor_id} />} />
        <Route path="/agents" element={<Agents role={user.role} />} />
        <Route path="/history" element={<History role={user.role} />} />
        <Route path="*" element={<Navigate to="/threads" replace />} />
      </Routes>
    </BrowserRouter>
  );
}

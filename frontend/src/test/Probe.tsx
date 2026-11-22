import { useEffect } from "react";
import { useLocation, useNavigate, type NavigateFunction } from "react-router";

// Renders the current URL into the DOM and hands the router's navigate out, so
// a test can read a search param and press "back" (MemoryRouter has no browser
// history to drive).
export function Probe({ onNavigate }: { onNavigate: (n: NavigateFunction) => void }) {
  const location = useLocation();
  const navigate = useNavigate();
  useEffect(() => onNavigate(navigate), [navigate, onNavigate]);
  return (
    <div hidden data-testid="location">
      {location.pathname + location.search}
    </div>
  );
}

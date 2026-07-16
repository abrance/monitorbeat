import { NavLink } from 'react-router-dom'

export function Nav() {
  return (
    <header className="nav">
      <div className="nav-brand">monitorbeat</div>
      <nav className="nav-links">
        <NavLink to="/">Overview</NavLink>
        <NavLink to="/probes">Probes</NavLink>
        <NavLink to="/alerts">Alerts</NavLink>
        <NavLink to="/alerts/history">History</NavLink>
      </nav>
    </header>
  )
}

import React from 'react'
import { createRoot } from 'react-dom/client'
import { HashRouter, Routes, Route } from 'react-router-dom'
import Overview from './pages/Overview'
import HostDetail from './pages/HostDetail'
import Probes from './pages/Probes'
import { Nav } from './components/Nav'
import './styles.css'

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <HashRouter>
      <Nav />
      <main className="container">
        <Routes>
          <Route path="/" element={<Overview />} />
          <Route path="/host/:hostname" element={<HostDetail />} />
          <Route path="/probes" element={<Probes />} />
        </Routes>
      </main>
    </HashRouter>
  </React.StrictMode>,
)

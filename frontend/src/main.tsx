import { lazy, StrictMode, Suspense } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'

const isPrototype = /^\/prototype(?:\/|$)/.test(window.location.pathname)
if (isPrototype) document.documentElement.dataset.singUi = 'prototype'
const App = isPrototype
  ? lazy(() => import('./prototype/PrototypeApp.tsx'))
  : lazy(() => import('./App.tsx'))

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <Suspense fallback={<div role="status" style={{ padding: 24 }}>正在加载 sing-ui…</div>}><App /></Suspense>
  </StrictMode>,
)

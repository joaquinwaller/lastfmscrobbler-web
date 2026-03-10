import { useEffect, useState } from 'react'

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || 'http://localhost:8080'
const STORAGE_KEY = 'lfm_auth'
const MAX_DAILY_SCROBBLES = 3000

function readAuthFromStorage() {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    return raw ? JSON.parse(raw) : null
  } catch {
    return null
  }
}

function parseAuthFromHash(hashValue) {
  const hash = hashValue.startsWith('#') ? hashValue.slice(1) : hashValue
  const params = new URLSearchParams(hash)
  const token = params.get('token')
  const userId = params.get('user_id')
  const username = params.get('username')

  if (!token || !userId || !username) {
    return null
  }

  return {
    token,
    user: { id: userId, username },
  }
}

function sameAuth(a, b) {
  if (!a || !b) {
    return false
  }
  return a.token === b.token && a.user?.id === b.user?.id
}

function clearStoredAuth() {
  localStorage.removeItem(STORAGE_KEY)
}

function persistAuth(nextAuth) {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(nextAuth))
}

async function apiPost(path, token, body = {}) {
  const headers = {
    'Content-Type': 'application/json',
  }
  if (token) {
    headers.Authorization = `Bearer ${token}`
  }

  const res = await fetch(`${API_BASE_URL.replace(/\/$/, '')}${path}`, {
    method: 'POST',
    headers,
    body: JSON.stringify(body),
  })

  const data = await res.json().catch(() => ({}))
  if (!res.ok) {
    throw new Error(data.error || `Request failed with ${res.status}`)
  }
  return data
}

async function apiRefresh(token) {
  return apiPost('/auth/refresh', token)
}

async function apiGet(path) {
  let res
  try {
    res = await fetch(`${API_BASE_URL.replace(/\/$/, '')}${path}`)
  } catch {
    throw new Error('Could not reach the API. Please try again in a few seconds.')
  }

  const data = await res.json().catch(() => ({}))
  if (!res.ok) {
    throw new Error(data.error || `Request failed with ${res.status}`)
  }
  return data
}

export default function App() {
  const [auth, setAuth] = useState(() => readAuthFromStorage())
  const [status, setStatus] = useState('')
  const [loginLoading, setLoginLoading] = useState(false)

  const [url, setURL] = useState('')
  const [amount, setAmount] = useState(100)
  const [preview, setPreview] = useState(null)
  const [previewLoading, setPreviewLoading] = useState(false)
  const [previewError, setPreviewError] = useState('')
  const [runLoading, setRunLoading] = useState(false)
  const [runResult, setRunResult] = useState(null)

  function applyAuth(nextAuth) {
    persistAuth(nextAuth)
    setAuth(nextAuth)
  }

  function resetAuth(message = 'Your session expired. Please log in again.') {
    clearStoredAuth()
    setAuth(null)
    setLoginLoading(false)
    setRunLoading(false)
    setPreviewLoading(false)
    setRunResult(null)
    setStatus(message)
  }

  async function runAuthedPost(path, body = {}) {
    if (!auth?.token) {
      resetAuth('Your session expired. Please log in again.')
      throw new Error('Authentication required')
    }

    try {
      return await apiPost(path, auth.token, body)
    } catch (err) {
      const message = String(err?.message || '')
      if (!message.includes('401') && !message.toLowerCase().includes('authorization') && !message.toLowerCase().includes('token')) {
        throw err
      }

      try {
        const refreshed = await apiRefresh(auth.token)
        if (!refreshed?.token || !refreshed?.user) {
          throw new Error('Invalid refresh response')
        }

        const nextAuth = { token: refreshed.token, user: refreshed.user }
        applyAuth(nextAuth)
        return await apiPost(path, nextAuth.token, body)
      } catch {
        resetAuth('Your session is no longer valid. Please log in again.')
        throw new Error('Your session is no longer valid. Please log in again.')
      }
    }
  }

  function handleAmountChange(event) {
    const nextValue = event.target.value
    if (nextValue === '') {
      setAmount('')
      return
    }

    const parsed = Number(nextValue)
    if (Number.isNaN(parsed)) {
      return
    }

    setAmount(Math.min(MAX_DAILY_SCROBBLES, Math.max(1, parsed)))
  }

  useEffect(() => {
    if (window.location.pathname !== '/auth/callback') {
      return
    }

    const nextAuth = parseAuthFromHash(window.location.hash)

    if (nextAuth) {
      persistAuth(nextAuth)
      if (window.opener && window.opener !== window) {
        window.opener.postMessage({ type: 'lastfm-auth-success', payload: nextAuth }, window.location.origin)
        window.close()
        return
      }
    } else if (window.opener && window.opener !== window) {
      window.opener.postMessage({ type: 'lastfm-auth-error' }, window.location.origin)
      window.close()
      return
    }

    setAuth(nextAuth)
    setStatus(nextAuth ? 'Last.fm connection completed.' : 'Login could not be completed. Please try again.')
    window.history.replaceState({}, '', '/')
  }, [])

  useEffect(() => {
    function handleMessage(event) {
      if (event.origin !== window.location.origin) {
        return
      }
      if (event.data?.type === 'lastfm-auth-success' && event.data?.payload) {
        applyAuth(event.data.payload)
        setStatus('Last.fm connection completed.')
        setLoginLoading(false)
      }
      if (event.data?.type === 'lastfm-auth-error') {
        setStatus('Login could not be completed. Please try again.')
        setLoginLoading(false)
      }
    }

    window.addEventListener('message', handleMessage)
    return () => window.removeEventListener('message', handleMessage)
  }, [])

  useEffect(() => {
    if (!loginLoading) {
      return
    }

    const startedAt = Date.now()
    const fallbackWatcher = window.setInterval(() => {
      const fromStorage = readAuthFromStorage()
      if (fromStorage && !sameAuth(auth, fromStorage)) {
        setAuth(fromStorage)
        setStatus('Last.fm connection completed.')
        setLoginLoading(false)
        window.clearInterval(fallbackWatcher)
        return
      }

      if (Date.now() - startedAt > 90_000) {
        setLoginLoading(false)
        setStatus('Login timeout. Close the authorization window and try again.')
        window.clearInterval(fallbackWatcher)
      }
    }, 500)

    return () => window.clearInterval(fallbackWatcher)
  }, [loginLoading, auth])

  useEffect(() => {
    if (!auth?.token) {
      return
    }

    let cancelled = false

    void (async () => {
      try {
        const refreshed = await apiRefresh(auth.token)
        if (cancelled || !refreshed?.token || !refreshed?.user) {
          return
        }

        const nextAuth = { token: refreshed.token, user: refreshed.user }
        if (!sameAuth(auth, nextAuth) || auth.user?.username !== nextAuth.user?.username) {
          applyAuth(nextAuth)
        }
      } catch {
        if (!cancelled) {
          resetAuth('Your saved session expired or became invalid. Please log in again.')
        }
      }
    })()

    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    if (!auth?.token) {
      return
    }

    const cleaned = url.trim()
    if (!cleaned) {
      setPreview(null)
      setPreviewError('')
      return
    }

    const timeout = window.setTimeout(async () => {
      setPreviewLoading(true)
      setPreviewError('')
      try {
        const data = await runAuthedPost('/scrobble/preview', { url: cleaned })
        setPreview(data.preview)
      } catch (err) {
        setPreview(null)
        setPreviewError(err.message)
      } finally {
        setPreviewLoading(false)
      }
    }, 450)

    return () => window.clearTimeout(timeout)
  }, [auth?.token, url])

  function logout() {
    clearStoredAuth()
    setAuth(null)
    setStatus('Signed out.')
    setURL('')
    setPreview(null)
    setPreviewError('')
    setRunResult(null)
    setLoginLoading(false)
  }

  function startLoginPopup() {
    if (auth?.token || loginLoading) {
      return
    }

    setStatus('')
    setLoginLoading(true)

    void (async () => {
      const width = 560
      const height = 760
      const left = Math.max(0, window.screenX + (window.outerWidth - width) / 2)
      const top = Math.max(0, window.screenY + (window.outerHeight - height) / 2)
      const features = `popup=yes,width=${width},height=${height},left=${Math.round(left)},top=${Math.round(top)}`

      let popup = window.open('', 'lastfm-auth', features)
      try {
        if (!popup) {
          throw new Error('Your browser blocked the popup. Please allow popups and try again.')
        }
        popup.document.title = 'Last.fm login'
        popup.document.body.innerHTML = '<p style="font-family: sans-serif; padding: 24px;">Connecting to Last.fm...</p>'

        const parsed = await apiGet('/auth/lastfm/start.json')
        if (!parsed.auth_url || !parsed.token) {
          throw new Error('Could not start Last.fm login')
        }

        popup.location.replace(parsed.auth_url)
        popup.focus()

        let consecutivePollFailures = 0
        const deadline = Date.now() + 90_000

        const poller = window.setInterval(async () => {
          if (Date.now() > deadline) {
            window.clearInterval(poller)
            setLoginLoading(false)
            setStatus('Login timeout. Close the authorization window and try again.')
            if (popup && !popup.closed) {
              popup.focus()
            }
            return
          }

          try {
            const data = await apiGet(`/auth/lastfm/poll?token=${encodeURIComponent(parsed.token)}`)
            consecutivePollFailures = 0
            if (!data?.token || !data?.user) {
              return
            }

            const nextAuth = { token: data.token, user: data.user }
            applyAuth(nextAuth)
            setStatus('Last.fm connection completed.')
            setLoginLoading(false)
            window.clearInterval(poller)
            if (popup && !popup.closed) {
              popup.close()
            }
          } catch (err) {
            if (String(err?.message || '').includes('Request failed with 202')) {
              consecutivePollFailures = 0
              return
            }

            consecutivePollFailures += 1
            if (consecutivePollFailures < 5) {
              return
            }

            window.clearInterval(poller)
            setLoginLoading(false)
            setStatus(err.message || 'Login error')
          }
        }, 1200)
      } catch (err) {
        setLoginLoading(false)
        setStatus(err.message || 'Could not start login')
        if (popup && !popup.closed) {
          popup.close()
        }
      }
    })()
  }

  async function startScrobble() {
    if (!auth?.token || !url.trim()) {
      return
    }

    setRunLoading(true)
    setRunResult(null)
    setStatus('')

    try {
      const data = await runAuthedPost('/scrobble/start', {
        url: url.trim(),
        amount: Math.min(MAX_DAILY_SCROBBLES, Number(amount) || 0),
      })
      setRunResult(data)
      setStatus('Scrobble completed.')
    } catch (err) {
      setStatus(err.message)
    } finally {
      setRunLoading(false)
    }
  }

  return (
    <main className="app-shell">
      <div className="ambient ambient-left" aria-hidden="true" />
      <div className="ambient ambient-right" aria-hidden="true" />
      <div className="page-content">
        <section className="hero panel stagger-1">
          {!auth ? (
            <div className="login-box">
              <p className="eyebrow">Last.fm Integration</p>
              <h1 className="brand">last.fm scrobbler</h1>
              <p className="subtitle">Connect your account to start scrobbling Spotify links in bulk.</p>
              <button className="cta" type="button" onClick={startLoginPopup} disabled={loginLoading}>
                <span className="cta-badge">fm</span>
                <span>{loginLoading ? 'Waiting for login...' : 'Login with Last.fm'}</span>
              </button>
            </div>
          ) : (
            <>
              <header className="top-header">
                <div>
                  <p className="eyebrow">Scrobble Dashboard</p>
                  <h1 className="brand">last.fm scrobbler</h1>
                  <p className="subtitle">Paste a Spotify URL, preview the tracks, then scrobble in bulk.</p>
                </div>
                <div className="top-actions">
                  <div className="connected-pill">Connected as @{auth.user.username}</div>
                  <button className="danger" type="button" onClick={logout}>
                    Sign out
                  </button>
                </div>
              </header>

              <section className="scrobble-box">
                <label className="field">
                  <span>Spotify URL</span>
                  <input
                    type="url"
                    value={url}
                    onChange={(e) => setURL(e.target.value)}
                    placeholder="https://open.spotify.com/playlist/..."
                  />
                </label>

                <article className="info-card">
                  <p className="info-card-title">Daily scrobble limit</p>
                  <p className="info-card-copy">
                    The limit is 3,000 scrobbles per day, and it resets daily at 00:00 GMT.
                  </p>
                </article>

                <div className="run-row">
                  <label className="field field-small">
                    <span>Amount</span>
                    <input
                      type="number"
                      min="1"
                      max={MAX_DAILY_SCROBBLES}
                      value={amount}
                      onChange={handleAmountChange}
                    />
                  </label>
                  <button
                    className="cta start"
                    type="button"
                    disabled={runLoading || previewLoading || !preview || !url.trim()}
                    onClick={startScrobble}
                  >
                    <span>{runLoading ? 'Scrobbling...' : 'Start Scrobble'}</span>
                  </button>
                </div>

                {previewLoading && <p className="status-note">Loading preview...</p>}
                {previewError && <p className="status-note error">{previewError}</p>}

                {preview && (
                  <article className="preview-card">
                    {preview.image ? <img src={preview.image} alt={preview.title} /> : <div className="cover-fallback">{preview.type}</div>}
                    <div className="preview-meta">
                      <p className="preview-type">{preview.type}</p>
                      <h3>{preview.title}</h3>
                      <p>{preview.subtitle}</p>
                      <p className="preview-count">{preview.total} tracks detected</p>
                      {preview.sample?.length > 0 && (
                        <ul>
                          {preview.sample.slice(0, 5).map((t, i) => (
                            <li key={`${t.name}-${i}`}>{t.artist} - {t.name}</li>
                          ))}
                        </ul>
                      )}
                    </div>
                  </article>
                )}

                {runResult && (
                  <article className="result-box">
                    <p><strong>Result:</strong> {runResult.sent}/{runResult.requested} sent</p>
                    {runResult.errors?.length > 0 && (
                      <p className="error">Errors: {runResult.errors.join(', ')}</p>
                    )}
                  </article>
                )}
              </section>
            </>
          )}

          {status && <p className="status-note">{status}</p>}
        </section>
      </div>

      <footer className="author-strip" />
    </main>
  )
}

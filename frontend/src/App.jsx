import { useEffect, useState } from 'react'

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || 'http://localhost:8080'
const STORAGE_KEY = 'lfm_auth'

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

export default function App() {
  const searchTypeOptions = [
    { value: 'artist', label: 'Artist' },
    { value: 'playlist', label: 'Playlist' },
    { value: 'album', label: 'Album' },
    { value: 'track', label: 'Track' },
  ]

  const [auth, setAuth] = useState(() => readAuthFromStorage())
  const [status, setStatus] = useState('')
  const [loginLoading, setLoginLoading] = useState(false)

  const [url, setURL] = useState('')
  const [inputMode, setInputMode] = useState('url')
  const [searchQuery, setSearchQuery] = useState('')
  const [searchLoading, setSearchLoading] = useState(false)
  const [searchResults, setSearchResults] = useState([])
  const [searchError, setSearchError] = useState('')
  const [searchType, setSearchType] = useState('artist')
  const [amount, setAmount] = useState(100)
  const [preview, setPreview] = useState(null)
  const [previewLoading, setPreviewLoading] = useState(false)
  const [previewError, setPreviewError] = useState('')
  const [runLoading, setRunLoading] = useState(false)
  const [runResult, setRunResult] = useState(null)

  useEffect(() => {
    if (window.location.pathname !== '/auth/callback') {
      return
    }

    const nextAuth = parseAuthFromHash(window.location.hash)

    if (nextAuth) {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(nextAuth))
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
        localStorage.setItem(STORAGE_KEY, JSON.stringify(event.data.payload))
        setAuth(event.data.payload)
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
    if (!auth?.token || inputMode !== 'search') {
      return
    }

    const q = searchQuery.trim()
    if (q.length < 2) {
      setSearchResults([])
      setSearchError('')
      setSearchLoading(false)
      return
    }

    const timeout = window.setTimeout(async () => {
      setSearchLoading(true)
      setSearchError('')
      try {
        const data = await apiPost('/scrobble/search', auth.token, { query: q, limit: 8, type: searchType })
        setSearchResults(Array.isArray(data.items) ? data.items : [])
      } catch (err) {
        setSearchResults([])
        setSearchError(err.message)
      } finally {
        setSearchLoading(false)
      }
    }, 350)

    return () => window.clearTimeout(timeout)
  }, [auth?.token, inputMode, searchQuery, searchType])

  useEffect(() => {
    setSearchResults([])
    setSearchError('')
  }, [searchType])

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
        const data = await apiPost('/scrobble/preview', auth.token, { url: cleaned })
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
    localStorage.removeItem(STORAGE_KEY)
    setAuth(null)
    setStatus('Signed out.')
    setURL('')
    setSearchQuery('')
    setSearchResults([])
    setSearchError('')
    setInputMode('url')
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

      let popup = null
      try {
        const startData = await fetch(`${API_BASE_URL.replace(/\/$/, '')}/auth/lastfm/start.json`)
        const parsed = await startData.json().catch(() => ({}))
        if (!startData.ok || !parsed.auth_url || !parsed.token) {
          throw new Error(parsed.error || 'Could not start Last.fm login')
        }

        popup = window.open(parsed.auth_url, 'lastfm-auth', features)
        if (!popup) {
          throw new Error('Your browser blocked the popup. Please allow popups and try again.')
        }
        popup.focus()

        const poller = window.setInterval(async () => {
          try {
            const res = await fetch(`${API_BASE_URL.replace(/\/$/, '')}/auth/lastfm/poll?token=${encodeURIComponent(parsed.token)}`)
            if (res.status === 202) {
              return
            }
            const data = await res.json().catch(() => ({}))
            if (!res.ok) {
              throw new Error(data.error || 'Authorization check failed')
            }
            if (data?.token && data?.user) {
              const nextAuth = { token: data.token, user: data.user }
              localStorage.setItem(STORAGE_KEY, JSON.stringify(nextAuth))
              setAuth(nextAuth)
              setStatus('Last.fm connection completed.')
              setLoginLoading(false)
              window.clearInterval(poller)
              if (popup && !popup.closed) {
                popup.close()
              }
            }
          } catch (err) {
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
      const data = await apiPost('/scrobble/start', auth.token, {
        url: url.trim(),
        amount: Number(amount) || 0,
      })
      setRunResult(data)
      setStatus('Scrobble completed.')
    } catch (err) {
      setStatus(err.message)
    } finally {
      setRunLoading(false)
    }
  }

  function pickSearchItem(item) {
    if (!item?.source_url) {
      return
    }
    setURL(item.source_url)
    setStatus(`Selected ${item.type}: ${item.title}`)
    setRunResult(null)
  }

  function searchPlaceholder(type) {
    switch (type) {
      case 'artist':
        return 'Search an artist (e.g. Drake)'
      case 'track':
        return 'Search a track'
      case 'playlist':
        return 'Search a playlist'
      case 'album':
        return 'Search an album'
      default:
        return 'Search Spotify'
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
              <p className="subtitle">
                Connect your account to start scrobbling Spotify links or search Spotify directly.
              </p>
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
                  <p className="subtitle">Pick a Spotify URL or find content from search, then scrobble in bulk.</p>
                </div>
                <div className="top-actions">
                  <div className="connected-pill">Connected as @{auth.user.username}</div>
                  <button className="danger" type="button" onClick={logout}>
                    Sign out
                  </button>
                </div>
              </header>

              <section className="scrobble-box">
                <div className="mode-row">
                  <button
                    type="button"
                    className={`mode-btn ${inputMode === 'url' ? 'active' : ''}`}
                    onClick={() => setInputMode('url')}
                  >
                    Use URL
                  </button>
                  <button
                    type="button"
                    className={`mode-btn ${inputMode === 'search' ? 'active' : ''}`}
                    onClick={() => setInputMode('search')}
                  >
                    Search Spotify
                  </button>
                </div>

                {inputMode === 'search' ? (
                  <div className="search-box">
                    <div className="search-query-row">
                      <label className="field field-search-query">
                        <span>Search query</span>
                        <input
                          type="text"
                          value={searchQuery}
                          onChange={(e) => setSearchQuery(e.target.value)}
                          placeholder={searchPlaceholder(searchType)}
                        />
                      </label>
                      <label className="field field-search-type">
                        <span>Filter</span>
                        <select value={searchType} onChange={(e) => setSearchType(e.target.value)}>
                          {searchTypeOptions.map((option) => (
                            <option key={option.value} value={option.value}>
                              {option.label}
                            </option>
                          ))}
                        </select>
                      </label>
                    </div>

                    {searchLoading && <p className="status-note">Searching Spotify...</p>}
                    {searchError && <p className="status-note error">{searchError}</p>}

                    {searchResults.length > 0 && (
                      <div className="search-results">
                        {searchResults.map((item) => (
                          <button
                            key={`${item.type}-${item.id}`}
                            type="button"
                            className={`search-item ${url === item.source_url ? 'selected' : ''}`}
                            onClick={() => pickSearchItem(item)}
                          >
                            {item.image ? <img src={item.image} alt={item.title} /> : <div className="cover-fallback">{item.type}</div>}
                            <div>
                              <p className="preview-type">{item.type}</p>
                              <p className="search-title">{item.title}</p>
                              <p className="search-subtitle">{item.subtitle}</p>
                            </div>
                          </button>
                        ))}
                      </div>
                    )}
                  </div>
                ) : null}

                <label className="field">
                  <span>Spotify URL</span>
                  <input
                    type="url"
                    value={url}
                    onChange={(e) => setURL(e.target.value)}
                    placeholder="https://open.spotify.com/playlist/..."
                  />
                </label>

                <div className="run-row">
                  <label className="field field-small">
                    <span>Amount</span>
                    <input
                      type="number"
                      min="1"
                      max="3000"
                      value={amount}
                      onChange={(e) => setAmount(e.target.value)}
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

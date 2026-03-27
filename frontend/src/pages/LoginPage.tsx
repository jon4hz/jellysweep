import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '@/context/AuthContext'
import { getAuthConfig, loginJellyfin } from '@/api/auth'
import type { AuthConfig } from '@/types/api'
import { LoadingSpinner } from '@/components/ui'

export default function LoginPage() {
  const { user, loading: authLoading, refresh } = useAuth()
  const navigate = useNavigate()
  const [authConfig, setAuthConfig] = useState<AuthConfig | null>(null)
  const [loading, setLoading] = useState(true)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!authLoading && user) {
      navigate('/', { replace: true })
      return
    }
    getAuthConfig()
      .then(setAuthConfig)
      .catch(() => setError('Failed to load auth configuration'))
      .finally(() => setLoading(false))
  }, [user, authLoading, navigate])

  async function handleJellyfinSubmit(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    setSubmitting(true)
    try {
      const data = await loginJellyfin(username, password)
      if (data.success) {
        await refresh()
        navigate(data.redirect ?? '/', { replace: true })
      } else {
        setError(data.error ?? 'Login failed')
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Login failed')
    } finally {
      setSubmitting(false)
    }
  }

  if (loading || authLoading) return <LoadingSpinner size="lg" />

  return (
    <div className="min-h-screen flex items-start justify-center pt-16 pb-4 px-4 sm:px-6 lg:px-8">
      <div className="max-w-md w-full space-y-6">
        <div className="text-center">
          <div className="mx-auto w-32 h-32 mb-4 flex items-center justify-center">
            <img src="/static/jellysweep.png" alt="Jellysweep" className="w-32 h-32" />
          </div>
          <h2 className="text-3xl font-bold text-gray-100">Welcome to Jellysweep</h2>
          <p className="mt-2 text-sm text-gray-400">Sign in to manage your media library</p>
        </div>

        <div className="card p-8">
          <div className="space-y-6">
            {error && (
              <div className="bg-red-900 border border-red-700 text-red-100 p-3 rounded-lg text-sm">
                {error}
              </div>
            )}

            {authConfig?.jellyfin?.enabled && (
              <form onSubmit={handleJellyfinSubmit} className="space-y-4">
                <div>
                  <label htmlFor="username" className="block text-sm font-medium text-gray-300">
                    Username
                  </label>
                  <input
                    type="text"
                    id="username"
                    required
                    value={username}
                    onChange={(e) => setUsername(e.target.value)}
                    className="mt-1 block w-full px-3 py-2 bg-gray-800 border border-gray-600 rounded-lg text-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
                  />
                </div>
                <div>
                  <label htmlFor="password" className="block text-sm font-medium text-gray-300">
                    Password
                  </label>
                  <input
                    type="password"
                    id="password"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    className="mt-1 block w-full px-3 py-2 bg-gray-800 border border-gray-600 rounded-lg text-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
                  />
                </div>
                <button
                  type="submit"
                  disabled={submitting}
                  className="w-full flex justify-center py-3 px-4 border border-transparent rounded-lg shadow-sm text-sm font-medium text-white bg-linear-to-r from-indigo-600 to-purple-600 hover:from-indigo-700 hover:to-purple-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-indigo-500 transition-all duration-200 disabled:opacity-50"
                >
                  {submitting ? (
                    <>
                      <svg className="w-5 h-5 mr-2 animate-spin" fill="none" viewBox="0 0 24 24">
                        <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                        <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
                      </svg>
                      Signing in...
                    </>
                  ) : (
                    <>
                      <svg className="w-5 h-5 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M11 16l-4-4m0 0l4-4m4 4H3m16 0a9 9 0 11-18 0 9 9 0 0118 0z" />
                      </svg>
                      Sign in with Jellyfin
                    </>
                  )}
                </button>
              </form>
            )}

            {authConfig?.jellyfin?.enabled && authConfig?.oidc?.enabled && (
              <div className="relative">
                <div className="absolute inset-0 flex items-center">
                  <div className="w-full border-t border-gray-600" />
                </div>
                <div className="relative flex justify-center text-sm">
                  <span className="px-2 bg-gray-900 text-gray-400">Or</span>
                </div>
              </div>
            )}

            {authConfig?.oidc?.enabled && (
              <a
                href="/auth/oidc/login"
                className="w-full flex justify-center py-3 px-4 border border-transparent rounded-lg shadow-sm text-sm font-medium text-white bg-linear-to-r from-indigo-600 to-purple-600 hover:from-indigo-700 hover:to-purple-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-indigo-500 transition-all duration-200"
              >
                <svg className="w-5 h-5 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z" />
                </svg>
                Sign in with {authConfig.oidc.name}
              </a>
            )}
          </div>
        </div>

        <div className="text-center">
          <p className="text-sm text-gray-400">Don&apos;t have access? Contact your administrator.</p>
        </div>
      </div>
    </div>
  )
}

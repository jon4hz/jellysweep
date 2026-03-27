import { createContext, useContext, useEffect, useState, useCallback, type ReactNode } from 'react'
import { getMe } from '@/api/auth'
import type { MeResponse } from '@/types/api'

interface AuthState {
  user: MeResponse | null
  loading: boolean
  error: string | null
}

interface AuthContextValue extends AuthState {
  refresh: () => Promise<void>
}

const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState>({
    user: null,
    loading: true,
    error: null,
  })

  const refresh = useCallback(async () => {
    try {
      const user = await getMe()
      setState({ user, loading: false, error: null })
    } catch {
      setState({ user: null, loading: false, error: null })
    }
  }, [])

  useEffect(() => {
    refresh()
  }, [refresh])

  return (
    <AuthContext.Provider value={{ ...state, refresh }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within AuthProvider')
  return ctx
}

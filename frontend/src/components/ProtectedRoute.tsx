import { Navigate, Outlet } from 'react-router-dom'
import { useAuth } from '@/context/AuthContext'
import { LoadingSpinner } from './ui'

export function ProtectedRoute({ requireAdmin = false }: { requireAdmin?: boolean }) {
  const { user, loading } = useAuth()

  if (loading) return <LoadingSpinner size="lg" />
  if (!user) return <Navigate to="/login" replace />
  if (requireAdmin && !user.isAdmin) return <Navigate to="/" replace />

  return <Outlet />
}

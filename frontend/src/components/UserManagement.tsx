import { useState, useEffect, useCallback } from 'react'
import type { UserPermission } from '@/types/api'
import { getUsers, updateUserPermissions } from '@/api/admin'
import { formatDate } from '@/lib/utils'
import { LoadingSpinner, showToast } from '@/components/ui'

export function UserManagement() {
  const [users, setUsers] = useState<UserPermission[]>([])
  const [loading, setLoading] = useState(true)
  const [toggling, setToggling] = useState<number | null>(null)

  const fetchUsers = useCallback(async () => {
    setLoading(true)
    try {
      const data = await getUsers()
      setUsers(data.users ?? [])
    } catch {
      // handled by client
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchUsers() }, [fetchUsers])

  const handleToggle = useCallback(async (user: UserPermission) => {
    if (toggling !== null) return
    setToggling(user.id)
    try {
      const data = await updateUserPermissions(user.id, !user.hasAutoApproval)
      if (data.success) {
        showToast(data.message, 'success')
        setUsers((prev) =>
          prev.map((u) => u.id === user.id ? { ...u, hasAutoApproval: !u.hasAutoApproval } : u),
        )
      }
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Failed to update permissions', 'error')
    } finally {
      setToggling(null)
    }
  }, [toggling])

  if (loading) {
    return (
      <div className="flex justify-center py-12">
        <LoadingSpinner size="lg" />
      </div>
    )
  }

  if (users.length === 0) {
    return (
      <div className="text-center py-12">
        <p className="text-gray-500">No users found.</p>
      </div>
    )
  }

  return (
    <div className="card overflow-hidden">
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-gray-700/50">
              <th className="text-left px-4 py-3 text-gray-400 font-medium">Username</th>
              <th className="text-left px-4 py-3 text-gray-400 font-medium">Created</th>
              <th className="text-left px-4 py-3 text-gray-400 font-medium">Auto-Approval</th>
              <th className="text-right px-4 py-3 text-gray-400 font-medium">Actions</th>
            </tr>
          </thead>
          <tbody>
            {users.map((user) => (
              <tr key={user.id} className="border-b border-gray-700/30 hover:bg-gray-800/50 transition-colors">
                <td className="px-4 py-3 text-gray-100 font-medium">{user.username}</td>
                <td className="px-4 py-3 text-gray-400">{formatDate(user.createdAt)}</td>
                <td className="px-4 py-3">
                  <span className={`px-2 py-0.5 text-xs font-medium rounded-full ${
                    user.hasAutoApproval
                      ? 'bg-green-500/20 text-green-400'
                      : 'bg-gray-500/20 text-gray-400'
                  }`}>
                    {user.hasAutoApproval ? 'Enabled' : 'Disabled'}
                  </span>
                </td>
                <td className="px-4 py-3 text-right">
                  <button
                    onClick={() => handleToggle(user)}
                    disabled={toggling === user.id}
                    className={`px-3 py-1 text-xs font-medium rounded-lg transition-colors disabled:opacity-50 ${
                      user.hasAutoApproval
                        ? 'bg-red-600/20 text-red-400 hover:bg-red-600/30'
                        : 'bg-green-600/20 text-green-400 hover:bg-green-600/30'
                    }`}
                  >
                    {toggling === user.id ? (
                      <span className="flex items-center gap-1">
                        <div className="animate-spin rounded-full h-3 w-3 border-b-2 border-current" />
                      </span>
                    ) : user.hasAutoApproval ? 'Disable' : 'Enable'}
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

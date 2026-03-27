import { useState, useEffect, useRef } from 'react'
import { Link, useLocation, Outlet } from 'react-router-dom'
import { useAuth } from '@/context/AuthContext'
import { ToastContainer } from './ui'

export function Layout() {
  const { user } = useAuth()

  return (
    <>
      <ToastContainer />
      {user && <Navbar />}
      <main className="container mx-auto px-4 py-8">
        <Outlet />
      </main>
    </>
  )
}

function Navbar() {
  const { user } = useAuth()
  const location = useLocation()
  const [mobileOpen, setMobileOpen] = useState(false)
  const [profileOpen, setProfileOpen] = useState(false)
  const profileRef = useRef<HTMLDivElement>(null)

  // Close dropdown on outside click
  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (profileRef.current && !profileRef.current.contains(e.target as Node)) {
        setProfileOpen(false)
      }
    }
    document.addEventListener('click', handleClick)
    return () => document.removeEventListener('click', handleClick)
  }, [])

  // Close mobile menu on navigation
  useEffect(() => {
    setMobileOpen(false)
  }, [location.pathname])

  if (!user) return null

  return (
    <>
      <nav className="bg-gray-900 border-b border-gray-800">
        <div className="container mx-auto px-4">
          <div className="flex justify-between items-center h-16">
            {/* Left side */}
            <div className="flex items-center space-x-4">
              <Link to="/" className="flex items-center space-x-2">
                <img src="/static/jellysweep.png" alt="Jellysweep" className="w-8 h-8 rounded-lg" />
                <span className="text-xl font-semibold text-gray-100">Jellysweep</span>
              </Link>

              {user.isDryRun && <DryRunBadge />}

              <div className="hidden md:flex space-x-6">
                <NavLink to="/" label="Dashboard" />
                {user.isAdmin && (
                  <>
                    <NavLink to="/admin" label="Admin Panel" badge={user.pendingRequestsCount > 0} />
                    <NavLink to="/admin/history" label="History" />
                    <NavLink to="/admin/scheduler" label="Scheduler" />
                  </>
                )}
              </div>
            </div>

            {/* Right side */}
            <div className="flex items-center space-x-4">
              {/* Mobile hamburger */}
              <button
                className="md:hidden text-gray-300 hover:text-white focus:outline-none"
                onClick={() => setMobileOpen(!mobileOpen)}
              >
                <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  {mobileOpen ? (
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                  ) : (
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 12h16M4 18h16" />
                  )}
                </svg>
              </button>

              {/* Desktop profile dropdown */}
              <div className="hidden md:flex items-center" ref={profileRef}>
                <button
                  className="flex items-center space-x-2 text-gray-300 hover:text-white transition-colors duration-200 focus:outline-none"
                  onClick={(e) => { e.stopPropagation(); setProfileOpen(!profileOpen) }}
                >
                  <div className="w-8 h-8 bg-gray-700 rounded-full flex items-center justify-center overflow-hidden">
                    {user.gravatarUrl ? (
                      <img src={user.gravatarUrl} alt={user.name} className="w-full h-full object-cover" />
                    ) : (
                      <svg className="w-4 h-4 text-gray-300" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" />
                      </svg>
                    )}
                  </div>
                  <span className="text-sm">{user.name}</span>
                  <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
                  </svg>
                </button>

                {profileOpen && (
                  <div className="absolute right-4 top-14 w-48 bg-gray-800 rounded-lg shadow-lg border border-gray-700 z-50">
                    <div className="py-2">
                      <div className="px-4 py-2 border-b border-gray-700">
                        <p className="text-gray-100 font-medium">{user.name}</p>
                        <p className="text-gray-400 text-sm">{user.isAdmin ? 'Administrator' : 'User'}</p>
                      </div>
                      <a
                        href="/logout"
                        className="flex items-center px-4 py-2 text-gray-300 hover:text-white hover:bg-gray-700 transition-colors duration-200"
                      >
                        <svg className="w-4 h-4 mr-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M17 16l4-4m0 0l-4-4m4 4H7m6 4v1a3 3 0 01-3 3H6a3 3 0 01-3-3V7a3 3 0 013-3h4a3 3 0 013 3v1" />
                        </svg>
                        Sign Out
                      </a>
                    </div>
                  </div>
                )}
              </div>
            </div>
          </div>
        </div>
      </nav>

      {/* Mobile side menu */}
      {mobileOpen && (
        <div className="fixed inset-0 z-50 md:hidden">
          <div className="fixed inset-0 bg-gray-900/20 backdrop-blur-sm" onClick={() => setMobileOpen(false)} />
          <div className="fixed right-0 top-0 h-full w-64 bg-gray-900 border-l border-gray-800 transform transition-transform duration-300 ease-in-out">
            <div className="flex flex-col h-full">
              <div className="flex items-center justify-between p-4 border-b border-gray-800">
                <span className="text-lg font-semibold text-gray-100">Menu</span>
                <button className="text-gray-300 hover:text-white" onClick={() => setMobileOpen(false)}>
                  <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                  </svg>
                </button>
              </div>

              {/* User info */}
              <div className="p-4 border-b border-gray-800">
                <div className="flex items-center space-x-3">
                  <div className="w-8 h-8 bg-gray-700 rounded-full flex items-center justify-center overflow-hidden">
                    {user.gravatarUrl ? (
                      <img src={user.gravatarUrl} alt={user.name} className="w-full h-full object-cover" />
                    ) : (
                      <svg className="w-4 h-4 text-gray-300" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" />
                      </svg>
                    )}
                  </div>
                  <div>
                    <p className="text-gray-100 font-medium">{user.name}</p>
                    <p className="text-gray-400 text-sm">{user.isAdmin ? 'Administrator' : 'User'}</p>
                  </div>
                </div>
              </div>

              {/* Nav links */}
              <div className="flex-1 py-4">
                <div className="space-y-2 px-4">
                  <MobileNavLink to="/" label="Dashboard" icon={
                    <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2H5a2 2 0 00-2-2z" />
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 5a2 2 0 012-2h4a2 2 0 012 2v2H8V5z" />
                    </svg>
                  } />
                  {user.isAdmin && (
                    <>
                      <MobileNavLink to="/admin" label="Admin Panel" badge={user.pendingRequestsCount} icon={
                        <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
                        </svg>
                      } />
                      <MobileNavLink to="/admin/history" label="History" icon={
                        <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
                        </svg>
                      } />
                      <MobileNavLink to="/admin/scheduler" label="Scheduler" icon={
                        <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
                        </svg>
                      } />
                    </>
                  )}
                </div>
              </div>

              {/* Logout */}
              <div className="p-4 border-t border-gray-800">
                <a
                  href="/logout"
                  className="flex items-center justify-center w-full px-4 py-3 text-gray-300 hover:text-white hover:bg-gray-800 rounded-lg transition-colors duration-200"
                >
                  <svg className="w-5 h-5 mr-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M17 16l4-4m0 0l-4-4m4 4H7m6 4v1a3 3 0 01-3 3H6a3 3 0 01-3-3V7a3 3 0 013-3h4a3 3 0 013 3v1" />
                  </svg>
                  Sign Out
                </a>
              </div>
            </div>
          </div>
        </div>
      )}
    </>
  )
}

// -- Helper sub-components --

function NavLink({ to, label, badge }: { to: string; label: string; badge?: boolean }) {
  return (
    <Link to={to} className="relative text-gray-300 hover:text-white transition-colors duration-200">
      {label}
      {badge && (
        <span className="absolute -top-1.5 -right-2.5 bg-red-500 rounded-full w-2.5 h-2.5 animate-pulse" />
      )}
    </Link>
  )
}

function MobileNavLink({ to, label, icon, badge }: { to: string; label: string; icon: React.ReactNode; badge?: number }) {
  return (
    <Link to={to} className="block px-4 py-3 text-gray-300 hover:text-white hover:bg-gray-800 rounded-lg transition-colors duration-200">
      <div className="flex items-center justify-between">
        <div className="flex items-center space-x-3">
          {icon}
          <span>{label}</span>
        </div>
        {badge !== undefined && badge > 0 && (
          <span className="bg-red-500 text-white rounded-full px-2 py-1 text-xs font-medium">{badge}</span>
        )}
      </div>
    </Link>
  )
}

function DryRunBadge() {
  return (
    <div className="group relative">
      <div className="flex items-center space-x-2 px-3 py-1 bg-yellow-900/50 border border-yellow-700 rounded-lg">
        <svg className="w-4 h-4 text-yellow-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
        </svg>
        <span className="text-xs font-medium text-yellow-400">DRY RUN</span>
      </div>
      <div className="absolute left-0 top-full mt-2 w-80 p-4 bg-gray-800 border border-gray-700 rounded-lg shadow-xl opacity-0 invisible group-hover:opacity-100 group-hover:visible transition-all duration-200 z-50">
        <div className="space-y-2">
          <div className="flex items-start space-x-2">
            <svg className="w-5 h-5 text-yellow-400 flex-shrink-0 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            <div>
              <p className="text-sm font-semibold text-gray-100 mb-1">Dry Run Mode Active</p>
              <p className="text-xs text-gray-300 mb-2">The application is running in dry run mode. No media items will be actually deleted.</p>
            </div>
          </div>
          <div className="flex items-start space-x-2 pt-2 border-t border-gray-700">
            <svg className="w-5 h-5 text-red-400 flex-shrink-0 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
            </svg>
            <div>
              <p className="text-xs font-semibold text-red-400 mb-1">Important</p>
              <p className="text-xs text-gray-300">
                If an item&apos;s deletion date is in the past (or a disk usage related delete policy applies) and you disable dry run mode, the item will be deleted <strong className="text-red-400">immediately</strong> without any grace period.
              </p>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

import { useRef, useEffect, useCallback, type ReactNode } from 'react'

interface Tab {
  id: string
  label: string
  icon?: ReactNode
}

interface TabContainerProps {
  tabs: Tab[]
  activeTab: string
  onTabChange: (id: string) => void
  children: ReactNode
}

export function TabContainer({ tabs, activeTab, onTabChange, children }: TabContainerProps) {
  const navRef = useRef<HTMLDivElement>(null)
  const sliderRef = useRef<HTMLDivElement>(null)
  const activeIndex = tabs.findIndex((t) => t.id === activeTab)

  const updateSlider = useCallback(() => {
    if (!navRef.current || !sliderRef.current) return
    const buttons = navRef.current.querySelectorAll<HTMLButtonElement>('.tab-btn')
    const active = buttons[activeIndex >= 0 ? activeIndex : 0]
    if (!active) return
    const navRect = navRef.current.getBoundingClientRect()
    const activeRect = active.getBoundingClientRect()
    sliderRef.current.style.width = `${active.offsetWidth}px`
    sliderRef.current.style.height = `${active.offsetHeight}px`
    sliderRef.current.style.left = `${activeRect.left - navRect.left}px`
    sliderRef.current.style.top = '0px'
  }, [activeIndex])

  useEffect(() => {
    updateSlider()
    window.addEventListener('resize', updateSlider)
    return () => window.removeEventListener('resize', updateSlider)
  }, [updateSlider])

  return (
    <div>
      <div className="flex justify-start mb-6 overflow-x-auto">
        <div className="inline-flex bg-gray-800/90 backdrop-blur-sm rounded-xl p-1.5 border border-gray-700/20 shadow-lg min-w-max">
          <nav className="flex space-x-1 relative" ref={navRef}>
            <div
              ref={sliderRef}
              className="absolute top-0 left-0 bg-linear-to-r from-indigo-600 to-indigo-500 rounded-lg transition-all duration-300 ease-in-out shadow-md"
              style={{ width: 0, height: 0, zIndex: 0 }}
            />
            {tabs.map((tab) => (
              <button
                key={tab.id}
                className={`tab-btn tab-button relative z-10 flex items-center px-3 sm:px-5 py-3 text-sm font-semibold rounded-lg transition-all duration-300 shadow-sm whitespace-nowrap ${
                  tab.id === activeTab
                    ? 'text-white'
                    : 'text-gray-400 hover:text-gray-200 hover:bg-gray-700/30'
                }`}
                onClick={() => onTabChange(tab.id)}
              >
                {tab.icon}
                <span>{tab.label}</span>
              </button>
            ))}
          </nav>
        </div>
      </div>
      <div className="pt-6">
        {children}
      </div>
    </div>
  )
}

interface TabContentProps {
  id: string
  activeTab: string
  children: ReactNode
}

export function TabContent({ id, activeTab, children }: TabContentProps) {
  if (id !== activeTab) return null
  return <>{children}</>
}

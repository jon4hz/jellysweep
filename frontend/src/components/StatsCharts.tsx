import { useEffect, useRef, useMemo } from 'react'
import {
  Chart,
  BarController,
  LineController,
  BarElement,
  LineElement,
  PointElement,
  CategoryScale,
  LinearScale,
  TimeScale,
  Tooltip,
  Legend,
  Filler,
} from 'chart.js'
import 'chartjs-adapter-date-fns'
import type { UserMediaItem } from '@/types/api'
import { formatFileSize } from '@/lib/utils'

Chart.register(
  BarController,
  LineController,
  BarElement,
  LineElement,
  PointElement,
  CategoryScale,
  LinearScale,
  TimeScale,
  Tooltip,
  Legend,
  Filler,
)

interface StatsChartsProps {
  items: UserMediaItem[]
}

function aggregateByDate(items: UserMediaItem[]) {
  const byDate: Record<string, number> = {}
  for (const item of items) {
    const date = new Date(item.DefaultDeleteAt).toISOString().split('T')[0]!
    byDate[date] = (byDate[date] ?? 0) + item.FileSize
  }
  const sorted = Object.entries(byDate).sort(([a], [b]) => a.localeCompare(b))
  return {
    labels: sorted.map(([d]) => d),
    data: sorted.map(([, v]) => v),
  }
}

export function StatsCharts({ items }: StatsChartsProps) {
  const dailyRef = useRef<HTMLCanvasElement>(null)
  const cumulativeRef = useRef<HTMLCanvasElement>(null)
  const dailyChartRef = useRef<Chart | null>(null)
  const cumulativeChartRef = useRef<Chart | null>(null)

  const agg = useMemo(() => aggregateByDate(items), [items])

  // Daily chart
  useEffect(() => {
    if (!dailyRef.current || agg.labels.length === 0) return
    dailyChartRef.current?.destroy()
    dailyChartRef.current = new Chart(dailyRef.current, {
      type: 'bar',
      data: {
        labels: agg.labels,
        datasets: [{
          label: 'Storage freed',
          data: agg.data,
          backgroundColor: 'rgba(99, 102, 241, 0.7)',
          borderColor: 'rgba(99, 102, 241, 1)',
          borderWidth: 1,
          borderRadius: 4,
        }],
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        plugins: {
          legend: { display: false },
          tooltip: {
            callbacks: {
              label: (ctx) => formatFileSize(ctx.parsed.y ?? 0),
            },
          },
        },
        scales: {
          x: {
            type: 'time',
            time: { unit: 'day', tooltipFormat: 'MMM d, yyyy' },
            grid: { color: 'rgba(55, 65, 81, 0.5)' },
            ticks: { color: '#9CA3AF' },
          },
          y: {
            beginAtZero: true,
            grid: { color: 'rgba(55, 65, 81, 0.5)' },
            ticks: {
              color: '#9CA3AF',
              callback: (val) => formatFileSize(val as number),
            },
          },
        },
      },
    })
    return () => { dailyChartRef.current?.destroy() }
  }, [agg])

  // Cumulative chart
  useEffect(() => {
    if (!cumulativeRef.current || agg.labels.length === 0) return
    const cumulative: number[] = []
    agg.data.reduce((acc, val) => {
      const sum = acc + val
      cumulative.push(sum)
      return sum
    }, 0)

    cumulativeChartRef.current?.destroy()
    cumulativeChartRef.current = new Chart(cumulativeRef.current, {
      type: 'line',
      data: {
        labels: agg.labels,
        datasets: [{
          label: 'Cumulative storage freed',
          data: cumulative,
          borderColor: 'rgba(168, 85, 247, 1)',
          backgroundColor: 'rgba(168, 85, 247, 0.1)',
          borderWidth: 2,
          fill: true,
          tension: 0.3,
          pointRadius: 3,
          pointBackgroundColor: 'rgba(168, 85, 247, 1)',
        }],
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        plugins: {
          legend: { display: false },
          tooltip: {
            callbacks: {
              label: (ctx) => formatFileSize(ctx.parsed.y ?? 0),
            },
          },
        },
        scales: {
          x: {
            type: 'time',
            time: { unit: 'day', tooltipFormat: 'MMM d, yyyy' },
            grid: { color: 'rgba(55, 65, 81, 0.5)' },
            ticks: { color: '#9CA3AF' },
          },
          y: {
            beginAtZero: true,
            grid: { color: 'rgba(55, 65, 81, 0.5)' },
            ticks: {
              color: '#9CA3AF',
              callback: (val) => formatFileSize(val as number),
            },
          },
        },
      },
    })
    return () => { cumulativeChartRef.current?.destroy() }
  }, [agg])

  if (items.length === 0) {
    return <p className="text-gray-500 text-center py-8">No data available for charts.</p>
  }

  return (
    <div className="space-y-8">
      <div className="card p-6">
        <h3 className="text-lg font-semibold text-gray-100 mb-4">Daily Cleanup Projection</h3>
        <div style={{ height: 300 }}>
          <canvas ref={dailyRef} />
        </div>
      </div>
      <div className="card p-6">
        <h3 className="text-lg font-semibold text-gray-100 mb-4">Cumulative Cleanup Projection</h3>
        <div style={{ height: 300 }}>
          <canvas ref={cumulativeRef} />
        </div>
      </div>
    </div>
  )
}

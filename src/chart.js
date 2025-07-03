// Chart.js bundle entry point
import {
  Chart,
  LineController,
  BarController,
  BarElement,
  LineElement,
  PointElement,
  LinearScale,
  TimeScale,
  Title,
  Tooltip,
  Legend,
  Filler,
} from "chart.js";

import "chartjs-adapter-date-fns";

// Register Chart.js components
Chart.register(
  LineController,
  BarController,
  BarElement,
  LineElement,
  PointElement,
  LinearScale,
  TimeScale,
  Title,
  Tooltip,
  Legend,
  Filler
);

// Make Chart globally available
window.Chart = Chart;

export default Chart;

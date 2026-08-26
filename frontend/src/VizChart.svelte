<script>
  import { onMount, onDestroy } from 'svelte';
  import { Chart, registerables, Colors } from 'chart.js';
  import { onThemeChange } from './lib/theme.js';

  // Register once at module level (Colors plugin auto-assigns dataset colors)
  Chart.register(...registerables, Colors);

  // Theme-aware Chart.js defaults — initialized from the current OS/app theme,
  // then updated on theme-change events.
  const startIsDark = typeof window !== 'undefined' && window.matchMedia
    ? document.documentElement.getAttribute('data-theme') === 'dark'
    : false;
  Chart.defaults.color = startIsDark ? '#b0b0c0' : '#666666';
  Chart.defaults.borderColor = startIsDark ? '#2e2e50' : '#e9ecef';
  Chart.defaults.font.family = 'system-ui, -apple-system, sans-serif';
  Chart.defaults.font.size = 12;

  // Theme-aware Chart.js defaults — called by the theme-change handler
  function applyChartTheme(isDark) {
    Chart.defaults.color = isDark ? '#b0b0c0' : '#666666';
    Chart.defaults.borderColor = isDark ? '#2e2e50' : '#e9ecef';
  }

  let { config, standalone = true } = $props();
  let canvas = $state(null);
  let chart = null;
  let error = $state(null);
  let themeTick = $state(0);

  $effect(() => {
    // Depends on both config and themeTick — theme change triggers re-run
    const _tick = themeTick;
    if (config && canvas) {
      if (chart) {
        // Disconnect old resize observer before destroying
        if (canvas?._resizeObserver) {
          canvas._resizeObserver.disconnect();
          canvas._resizeObserver = null;
        }
        chart.destroy();
        chart = null;
      }
      try {
        const cfg = typeof config === 'string' ? JSON.parse(config) : config;
        if (!cfg || !cfg.type) {
          error = 'Missing chart type';
          return;
        }
        if (!cfg.options) cfg.options = {};
        // Responsive mode disabled — CSS handles sizing via width: 100%.
        // Chart.js's ResizeObserver fights CSS overrides, causing overflow.
        cfg.options.responsive = false;
        cfg.options.maintainAspectRatio = false;

        chart = new Chart(canvas, cfg);
        error = null;

        // Manual resize: since responsive is off, watch the container and
        // resize the chart when it changes.
        const container = canvas.parentElement;
        const ro = new ResizeObserver(() => {
          if (chart) {
            chart.resize(container.clientWidth, container.clientHeight);
          }
        });
        ro.observe(container);
        // Store for cleanup
        canvas._resizeObserver = ro;
      } catch (e) {
        error = String(e);
      }
    }
  });

  onMount(() => {
    const unsubscribe = onThemeChange((resolved) => {
      applyChartTheme(resolved === 'dark');
      themeTick++;
    });
    return unsubscribe;
  });

  onDestroy(() => {
    if (chart) {
      chart.destroy();
      chart = null;
    }
    // Disconnect resize observer if present
    if (canvas?._resizeObserver) {
      canvas._resizeObserver.disconnect();
    }
  });
</script>

{#if config && !error}
  <div class="viz-chart-container" class:standalone class:embedded={!standalone}>
    <canvas bind:this={canvas}></canvas>
  </div>
{:else if error}
  <div class="viz-chart-container" class:standalone class:embedded={!standalone}>
    <div class="viz-error">Chart: {error}</div>
  </div>
{/if}

<style>
  .viz-chart-container.standalone {
    position: relative;
    width: 100%;
    min-height: 380px;
    margin: 0.75rem 0;
    padding: 1rem;
    background: var(--bg-surface);
    border-radius: 8px;
    border: 1px solid var(--border-primary);
  }
  .viz-chart-container.embedded {
    position: relative;
    width: 100%;
    height: 380px;
    overflow: hidden;
  }
  canvas {
    width: 100% !important;
  }
  .viz-error {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 380px;
    color: var(--text-tertiary);
    font-size: 0.875rem;
  }
</style>

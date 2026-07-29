<script>
  import { onMount, onDestroy } from 'svelte';
  import { Chart, registerables, Colors } from 'chart.js';

  // Register once at module level (Colors plugin auto-assigns dataset colors)
  Chart.register(...registerables, Colors);
  Chart.defaults.borderColor = '#e9ecef';
  Chart.defaults.font.family = 'system-ui, -apple-system, sans-serif';
  Chart.defaults.font.size = 12;

  // Theme-aware Chart.js defaults — called by the theme-change handler
  function applyChartTheme(isDark) {
    Chart.defaults.color = isDark ? '#b0b0c0' : '#666666';
    Chart.defaults.borderColor = isDark ? '#2e2e50' : '#e9ecef';
  }

  let { config } = $props();
  let canvas = $state(null);
  let chart = null;
  let error = $state(null);
  let themeTick = $state(0);

  $effect(() => {
    // Depends on both config and themeTick — theme change triggers re-run
    const _tick = themeTick;
    if (config && canvas) {
      if (chart) {
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
        cfg.options.responsive = true;
        cfg.options.maintainAspectRatio = false;

        chart = new Chart(canvas, cfg);
        error = null;
      } catch (e) {
        error = String(e);
      }
    }
  });

  onMount(() => {
    const handler = (e) => {
      applyChartTheme(e.detail.theme === 'dark');
      themeTick++;
    };
    window.addEventListener('theme-change', handler);
    return () => window.removeEventListener('theme-change', handler);
  });

  onDestroy(() => {
    if (chart) {
      chart.destroy();
      chart = null;
    }
  });
</script>

{#if config && !error}
  <div class="viz-chart-container">
    <canvas bind:this={canvas}></canvas>
  </div>
{:else if error}
  <div class="viz-chart-container">
    <div class="viz-error">Chart: {error}</div>
  </div>
{/if}

<style>
  .viz-chart-container {
    position: relative;
    width: 100%;
    min-height: 380px;
    margin: 0.75rem 0;
    padding: 1rem;
    background: var(--bg-surface);
    border-radius: 8px;
    border: 1px solid var(--border-primary);
  }
  canvas {
    width: 100% !important;
    min-height: 340px !important;
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

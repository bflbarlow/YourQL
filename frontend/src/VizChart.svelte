<script>
  import { onDestroy } from 'svelte';

  let { config } = $props();
  let canvas = $state(null);
  let chart = null;
  let error = $state(null);

  $effect(() => {
    if (config && canvas) {
      console.log('[VizChart] canvas ready, initializing chart...', { config });
      initChart();
    }
  });

  async function initChart() {
    if (!canvas) return;
    try {
      const { Chart, registerables } = await import('chart.js');
      Chart.register(...registerables);

      if (chart) {
        chart.destroy();
        chart = null;
      }

      const cfg = typeof config === 'string' ? JSON.parse(config) : config;
      console.log('[VizChart] chart config:', cfg);

      if (!cfg || !cfg.type) {
        console.warn('[VizChart] missing chart type');
        return;
      }

      if (!cfg.options) cfg.options = {};
      cfg.options.responsive = true;
      cfg.options.maintainAspectRatio = false;

      chart = new Chart(canvas, cfg);
      error = null;
      console.log('[VizChart] chart created successfully');
    } catch (e) {
      error = String(e);
      console.error('[VizChart] render failed:', e);
    }
  }

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
    <div class="viz-error">Chart unavailable: {error}</div>
  </div>
{/if}

<style>
  .viz-chart-container {
    position: relative;
    width: 100%;
    min-height: 380px;
    margin: 0.75rem 0;
    padding: 1rem;
    background: var(--bg-secondary, #1e1e1e);
    border-radius: 8px;
    border: 1px solid var(--border-color, #333);
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
    color: #999;
    font-size: 0.875rem;
  }
</style>
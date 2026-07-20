<script>
  import { onDestroy } from 'svelte';
  import { Chart, registerables } from 'chart.js';

  // Register once at module level
  Chart.register(...registerables);
  Chart.defaults.borderColor = '#e9ecef';
  Chart.defaults.font.family = 'system-ui, -apple-system, sans-serif';
  Chart.defaults.font.size = 12;

  let { config } = $props();
  let canvas = $state(null);
  let chart = null;
  let error = $state(null);

  $effect(() => {
    if (config && canvas) {
      console.log('[VizChart] canvas ready, initializing chart...', config);
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
        console.log('[VizChart] chart created:', cfg.type);
      } catch (e) {
        error = String(e);
        console.error('[VizChart] render failed:', e);
      }
    }
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
    background: #f8f9fa;
    border-radius: 8px;
    border: 1px solid #e9ecef;
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
    color: #adb5bd;
    font-size: 0.875rem;
  }
</style>
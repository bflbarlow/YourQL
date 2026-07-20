<script>
  import { onMount, onDestroy } from 'svelte';
  import { Chart, registerables } from 'chart.js';

  Chart.register(...registerables);

  let { config } = $props();
  let canvas;
  let chart = $state(null);

  onMount(() => {
    renderChart();
  });

  onDestroy(() => {
    if (chart) {
      chart.destroy();
      chart = null;
    }
  });

  $effect(() => {
    // Re-render when config changes
    if (config) {
      renderChart();
    }
  });

  function renderChart() {
    if (!canvas || !config) return;
    if (chart) {
      chart.destroy();
      chart = null;
    }
    try {
      const cfg = typeof config === 'string' ? JSON.parse(config) : config;
      // Ensure responsive defaults
      if (!cfg.options) cfg.options = {};
      if (cfg.options.responsive === undefined) cfg.options.responsive = true;
      cfg.options.maintainAspectRatio = false;
      chart = new Chart(canvas, cfg);
    } catch (e) {
      console.error('Failed to render chart:', e);
    }
  }
</script>

{#if config}
  <div class="viz-chart-container">
    <canvas bind:this={canvas}></canvas>
  </div>
{/if}

<style>
  .viz-chart-container {
    position: relative;
    width: 100%;
    height: 380px;
    margin: 0.75rem 0;
    padding: 1rem;
    background: var(--bg-secondary, #1e1e1e);
    border-radius: 8px;
    border: 1px solid var(--border-color, #333);
  }
  canvas {
    width: 100% !important;
    height: 100% !important;
  }
</style>
<script>
  import { onDestroy } from 'svelte';

  let { config } = $props();
  let canvas = $state(null);
  let chart = null;
  let error = $state(null);

  async function initChart() {
    if (!canvas || !config) return;
    try {
      // Dynamically import Chart.js to avoid module-level side effects
      const { Chart, registerables } = await import('chart.js');
      Chart.register(...registerables);

      if (chart) {
        chart.destroy();
        chart = null;
      }

      const cfg = typeof config === 'string' ? JSON.parse(config) : config;
      if (!cfg || !cfg.type) return;

      // Ensure responsive defaults
      if (!cfg.options) cfg.options = {};
      cfg.options.responsive = true;
      cfg.options.maintainAspectRatio = false;

      chart = new Chart(canvas, cfg);
      error = null;
    } catch (e) {
      error = String(e);
      console.error('VizChart render failed:', e);
    }
  }

  // Watch for config or canvas changes
  $effect(() => {
    if (config && canvas) {
      initChart();
    }
  });

  onDestroy(() => {
    if (chart) {
      chart.destroy();
      chart = null;
    }
  });

  // Bind canvas ref
  function setCanvas(el) {
    canvas = el;
  }
</script>

{#if config}
  <div class="viz-chart-container">
    {#if error}
      <div class="viz-error">Chart unavailable: {error}</div>
    {:else}
      <canvas bind:this={setCanvas}></canvas>
    {/if}
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
  .viz-error {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100%;
    color: #999;
    font-size: 0.875rem;
  }
</style>
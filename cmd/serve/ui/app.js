
function toggleInfo(btn) {
  btn.closest('.card').querySelector('.card-info').classList.toggle('visible');
  btn.classList.toggle('active');
}

// ── state ────────────────────────────────────────────────────────────────
let currentView = 'rates';
let windowDays = 7;
let rangeStart = (() => {
  const d = new Date();
  d.setUTCHours(0, 0, 0, 0);
  d.setUTCDate(d.getUTCDate() - (windowDays - 1));
  return d;
})();
let rangeEndDate = (() => { const d = new Date(); d.setUTCHours(0,0,0,0); return d; })();

let ratesChart, ratesChartExport, consumptionChart, consumptionChartExport, batteryChart, chargingChart;
let smartChart, smartCumulativeChart;
let cmpImport = 'go';    // 'go' | 'agile'
let cmpExport = 'fixed'; // 'fixed' | 'agile'
let includeStandingCharge = false;
let lastAnalysisData = null;
let batteryLoaded = false;
let chargingLoaded = false;
let smartChargingLoaded = false;
let lastSmartChargingData = null;
let currentRegion = 'E'; // default DNO region, updated from /api/config

// Custom plugin: draws horizontal and/or vertical threshold lines on any chart.
Chart.register({
  id: 'thresholdLines',
  afterDraw(chart, _, opts) {
    const { depletionY, breakevenX } = opts;
    if (depletionY == null && breakevenX == null) return;
    const { top, bottom, left, right } = chart.chartArea;
    const ctx = chart.ctx;
    ctx.save();
    ctx.lineWidth = 1.5;
    ctx.setLineDash([4, 4]);
    if (depletionY != null && chart.scales.y) {
      const y = chart.scales.y.getPixelForValue(depletionY);
      ctx.strokeStyle = '#f59e0b';
      ctx.beginPath(); ctx.moveTo(left, y); ctx.lineTo(right, y); ctx.stroke();
    }
    if (breakevenX != null && chart.scales.x) {
      const x = chart.scales.x.getPixelForValue(breakevenX);
      ctx.strokeStyle = '#a78bfa';
      ctx.beginPath(); ctx.moveTo(x, top); ctx.lineTo(x, bottom); ctx.stroke();
    }
    ctx.restore();
  },
});

// Custom plugin: dims the non-selected region of an overview chart to show the zoomed window.
Chart.register({
  id: 'zoomHighlight',
  afterDatasetsDraw(chart, _, opts) {
    if (opts == null || opts.min == null) return;
    const xScale = chart.scales.x;
    if (!xScale) return;
    const { left, right, top, bottom } = chart.chartArea;
    const x1 = Math.max(left, xScale.getPixelForValue(opts.min));
    const x2 = Math.min(right, xScale.getPixelForValue(opts.max));
    const ctx = chart.ctx;
    ctx.save();
    ctx.fillStyle = 'rgba(0,0,0,0.55)';
    if (x1 > left) ctx.fillRect(left, top, x1 - left, bottom - top);
    if (x2 < right) ctx.fillRect(x2, top, right - x2, bottom - top);
    ctx.strokeStyle = '#3b82f6';
    ctx.lineWidth = 1;
    ctx.strokeRect(x1, top, x2 - x1, bottom - top);
    ctx.restore();
  },
});

// Custom plugin: draws a vertical dashed line at a given bar index.
Chart.register({
  id: 'verticalLine',
  afterDraw(chart, _, opts) {
    if (opts.index == null || opts.index < 0) return;
    const meta = chart.getDatasetMeta(0);
    const bar  = meta.data[opts.index];
    if (!bar) return;
    const { top, bottom } = chart.chartArea;
    const ctx = chart.ctx;
    ctx.save();
    ctx.beginPath();
    ctx.strokeStyle = '#f59e0b';
    ctx.lineWidth = 1.5;
    ctx.setLineDash([4, 4]);
    ctx.moveTo(bar.x, top);
    ctx.lineTo(bar.x, bottom);
    ctx.stroke();
    ctx.restore();
  },
});

// Custom plugin: draws vertical dashed lines at tariff switch dates with a label,
// and optionally a static label at the left edge showing the initial tariff.
Chart.register({
  id: 'tariffSwitches',
  afterDraw(chart, _, opts) {
    const lines   = (opts && opts.lines)   || [];
    const initial = opts && opts.initial;
    if (!lines.length && !initial) return;
    const xScale = chart.scales.x;
    if (!xScale) return;
    const { top, bottom, left } = chart.chartArea;
    const ctx = chart.ctx;
    ctx.save();
    ctx.font = "10px 'JetBrains Mono','SF Mono',monospace";
    ctx.textAlign = 'left';
    if (initial) {
      ctx.fillStyle = '#a78bfa';
      ctx.fillText(initial, left + 4, top + 12);
    }
    ctx.setLineDash([4, 4]);
    ctx.lineWidth = 1.5;
    for (const { date, label } of lines) {
      const x = xScale.getPixelForValue(date);
      if (x == null || isNaN(x)) continue;
      ctx.strokeStyle = '#a78bfa';
      ctx.beginPath(); ctx.moveTo(x, top); ctx.lineTo(x, bottom); ctx.stroke();
      ctx.setLineDash([]);
      ctx.fillStyle = '#a78bfa';
      ctx.fillText(label, x + 3, top + 12);
      ctx.setLineDash([4, 4]);
    }
    ctx.restore();
  },
});

function cmpImportCost(day) {
  return cmpImport === 'agile' ? day.agile_import : day.go_import;
}
function cmpExportCost(day) {
  return cmpExport === 'agile' ? day.agile_export : day.go_export;
}
function cmpNet(day) {
  return cmpImportCost(day) - cmpExportCost(day);
}

function cmpLabel() {
  const i = cmpImport === 'agile' ? 'Agile import' : 'Go import';
  const e = cmpExport === 'agile' ? 'Agile export' : 'Fixed export';
  return i + ' / ' + e;
}

// ── helpers ──────────────────────────────────────────────────────────────
const fmtDate = d => d.toISOString().slice(0, 10);

function rangeEnd() { return rangeEndDate; }

function today() {
  const d = new Date();
  d.setUTCHours(0, 0, 0, 0);
  return d;
}

function rateColor(v) {
  if (v < 0)  return '#1a7a1a';
  if (v < 10) return '#2ecc40';
  if (v < 20) return '#b5ea4a';
  if (v < 30) return '#ffdc00';
  if (v < 40) return '#ff851b';
  return '#ff4136';
}

const chartDefaults = {
  animation: false,
  responsive: true,
  maintainAspectRatio: false,
  plugins: { legend: { display: false } },
};

const zoomConfig = {
  zoom: {
    drag: { enabled: true, backgroundColor: 'rgba(59,130,246,0.1)', borderColor: '#3b82f640', borderWidth: 1 },
    mode: 'x',
    onZoomComplete: ({ chart }) => {
      document.getElementById('reset-zoom').style.display = '';
      const overview = Chart.getChart(chart.canvas.id + '-overview');
      if (overview) {
        overview.options.plugins.zoomHighlight = { min: chart.scales.x.min, max: chart.scales.x.max };
        overview.update('none');
      }
    },
  },
};

function resetAllZoom() {
  [ratesChart, ratesChartExport, consumptionChart, consumptionChartExport, analysisChart, savingChart, batteryChart, chargingChart]
    .forEach(c => {
      if (!c) return;
      c.resetZoom();
      const overview = Chart.getChart(c.canvas.id + '-overview');
      if (overview) {
        overview.options.plugins.zoomHighlight = {};
        overview.update('none');
      }
    });
  document.getElementById('reset-zoom').style.display = 'none';
}

function createOverview(mainChart) {
  const overviewId = mainChart.canvas.id + '-overview';
  const el = document.getElementById(overviewId);
  if (!el) return;
  const existing = Chart.getChart(el);
  if (existing) existing.destroy();

  const isTime = mainChart.options.scales.x && mainChart.options.scales.x.type === 'time';
  const hasY1  = !!(mainChart.options.scales && mainChart.options.scales.y1);

  const datasets = mainChart.data.datasets.map(ds => {
    const d = {
      data: ds.data,
      backgroundColor: ds.backgroundColor,
      borderColor: ds.borderColor,
      borderWidth: 0,
      pointRadius: 0,
      fill: ds.fill || false,
      tension: ds.tension || 0,
      barPercentage: ds.barPercentage,
      categoryPercentage: ds.categoryPercentage,
    };
    if (ds.type)     d.type     = ds.type;
    if (ds.yAxisID)  d.yAxisID  = ds.yAxisID;
    if (ds.borderDash) d.borderDash = ds.borderDash;
    return d;
  });

  const scales = {
    x: { display: false, ...(isTime ? { type: 'time' } : {}) },
    y: { display: false },
  };
  if (hasY1) scales.y1 = { display: false, position: 'right',
    min: mainChart.options.scales.y1.min, max: mainChart.options.scales.y1.max };

  new Chart(el, {
    type: mainChart.config.type,
    data: { labels: mainChart.data.labels, datasets },
    options: {
      animation: false,
      responsive: true,
      maintainAspectRatio: false,
      events: [],
      plugins: {
        legend: { display: false },
        tooltip: { enabled: false },
        zoom: { zoom: { drag: { enabled: false } } },
        zoomHighlight: {},
      },
      scales,
    },
  });
}

const darkAxis = {
  ticks: { color: '#6b7280', font: { family: "'JetBrains Mono','SF Mono',monospace", size: 10 } },
  grid: { color: '#1e1e2e' },
  border: { color: '#2e2e3e' },
};

function timeAxis() {
  return {
    ...darkAxis,
    type: 'time',
    time: { unit: 'day', tooltipFormat: 'dd MMM HH:mm' },
    ticks: { ...darkAxis.ticks, maxTicksLimit: 7 },
  };
}

// ── charts ───────────────────────────────────────────────────────────────
function makeRatesChart(canvasId, points) {
  return new Chart(document.getElementById(canvasId), {
    type: 'bar',
    data: {
      datasets: [{
        data: points.map(p => ({ x: p.t, y: p.v })),
        backgroundColor: points.map(p => rateColor(p.v)),
        borderWidth: 0,
        barPercentage: 1.0,
        categoryPercentage: 1.0,
      }]
    },
    options: {
      ...chartDefaults,
      scales: {
        x: timeAxis(),
        y: {
          ...darkAxis,
          ticks: { ...darkAxis.ticks, callback: v => v + 'p' },
        },
      },
      plugins: {
        ...chartDefaults.plugins,
        zoom: zoomConfig,
        tooltip: {
          callbacks: {
            label: ctx => ` ${ctx.parsed.y.toFixed(2)}p/kWh`,
          },
        },
      },
    },
  });
}

function renderRates(importPoints, exportPoints) {
  if (ratesChart)       ratesChart.destroy();
  if (ratesChartExport) ratesChartExport.destroy();

  const vals = importPoints.map(p => p.v);
  const avg = vals.length ? (vals.reduce((a,b) => a+b, 0) / vals.length) : 0;
  const mn  = vals.length ? Math.min(...vals) : 0;
  const mx  = vals.length ? Math.max(...vals) : 0;
  const cheap = vals.filter(v => v < 10).length;

  document.getElementById('stat-avg').innerHTML   = avg.toFixed(1)  + '<span class="stat-unit">p</span>';
  document.getElementById('stat-min').innerHTML   = mn.toFixed(1)   + '<span class="stat-unit">p</span>';
  document.getElementById('stat-max').innerHTML   = mx.toFixed(1)   + '<span class="stat-unit">p</span>';
  document.getElementById('stat-cheap').textContent = cheap;

  ratesChart       = makeRatesChart('rates-chart',        importPoints);
  ratesChartExport = makeRatesChart('rates-chart-export', exportPoints);
  createOverview(ratesChart);
  createOverview(ratesChartExport);
}

function makeConsumptionChart(canvasId, points) {
  return new Chart(document.getElementById(canvasId), {
    type: 'bar',
    data: {
      datasets: [{
        data: points.map(p => ({ x: p.t, y: p.v })),
        backgroundColor: '#3b82f680',
        borderColor: '#3b82f6',
        borderWidth: 1,
        barPercentage: 1.0,
        categoryPercentage: 1.0,
      }]
    },
    options: {
      ...chartDefaults,
      scales: {
        x: timeAxis(),
        y: {
          ...darkAxis,
          ticks: { ...darkAxis.ticks, callback: v => v.toFixed(2) },
        },
      },
      plugins: {
        ...chartDefaults.plugins,
        zoom: zoomConfig,
        tooltip: {
          callbacks: {
            label: ctx => ` ${ctx.parsed.y.toFixed(3)} kWh`,
          },
        },
      },
    },
  });
}

function renderConsumption(importPoints, exportPoints) {
  if (consumptionChart)       consumptionChart.destroy();
  if (consumptionChartExport) consumptionChartExport.destroy();

  const vals  = importPoints.map(p => p.v);
  const total = vals.reduce((a,b) => a+b, 0);
  const days  = (rangeEnd() - rangeStart) / 86400000 + 1;
  const peak  = vals.length ? Math.max(...vals) : 0;

  document.getElementById('stat-total').innerHTML = total.toFixed(2) + '<span class="stat-unit">kWh</span>';
  document.getElementById('stat-daily').innerHTML = (total / days).toFixed(2) + '<span class="stat-unit">kWh</span>';
  document.getElementById('stat-peak').innerHTML  = peak.toFixed(3)  + '<span class="stat-unit">kWh</span>';

  consumptionChart       = makeConsumptionChart('consumption-chart',        importPoints);
  consumptionChartExport = makeConsumptionChart('consumption-chart-export', exportPoints);
  createOverview(consumptionChart);
  createOverview(consumptionChartExport);
}

// ── loading indicator ────────────────────────────────────────────────────
const loader = document.getElementById('loader');
let pending = 0;
function setLoading(on) {
  pending = Math.max(0, pending + (on ? 1 : -1));
  loader.classList.toggle('active', pending > 0);
}

// ── data loading ─────────────────────────────────────────────────────────
async function loadData() {
  const from = fmtDate(rangeStart);
  const to   = fmtDate(rangeEnd());
  document.getElementById('range-from').value = from;
  document.getElementById('range-to').value   = to;
  document.getElementById('header-sub').textContent = from + ' – ' + to;

  const region = currentRegion;
  const qsImport = `direction=import&region=${region}&from=${from}&to=${to}`;
  const qsExport = `direction=export&region=${region}&from=${from}&to=${to}`;
  setLoading(true);
  try {
    const [ratesImpRes, ratesExpRes, consImpRes, consExpRes] = await Promise.all([
      fetch(`/api/rates?${qsImport}`),
      fetch(`/api/rates?${qsExport}`),
      fetch(`/api/consumption?${qsImport}`),
      fetch(`/api/consumption?${qsExport}`),
    ]);
    const [ratesImp, ratesExp, consImp, consExp] = await Promise.all([
      ratesImpRes.ok ? ratesImpRes.json() : [],
      ratesExpRes.ok ? ratesExpRes.json() : [],
      consImpRes.ok  ? consImpRes.json()  : [],
      consExpRes.ok  ? consExpRes.json()  : [],
    ]);
    renderRates(ratesImp || [], ratesExp || []);
    renderConsumption(consImp || [], consExp || []);
    if (currentView === 'analysis') loadAnalysis();
    smartChargingLoaded = false;
    if (currentView === 'smart') loadSmartCharging();
  } finally {
    setLoading(false);
  }
}

// ── view switching ───────────────────────────────────────────────────────
function switchView(view) {
  currentView = view;
  document.getElementById('view-rates').style.display       = view === 'rates'       ? '' : 'none';
  document.getElementById('view-consumption').style.display = view === 'consumption' ? '' : 'none';
  document.getElementById('view-analysis').style.display    = view === 'analysis'    ? '' : 'none';
  document.getElementById('view-battery').style.display     = view === 'battery'     ? '' : 'none';
  document.getElementById('view-charging').style.display    = view === 'charging'    ? '' : 'none';
  const viewSmart = document.getElementById('view-smart');
if (viewSmart) viewSmart.style.display = view === 'smart' ? '' : 'none';
  document.querySelectorAll('.tab').forEach(t =>
    t.classList.toggle('active', t.dataset.view === view));
  const dateControls = view !== 'battery' && view !== 'charging';
  document.getElementById('nav-arrows').style.visibility  = dateControls ? '' : 'hidden';
  document.querySelector('.window-tabs').style.visibility  = dateControls ? '' : 'hidden';
}

// ── analysis ─────────────────────────────────────────────────────────────
let analysisChart, savingChart;

function pence(v) { return '£' + (v / 100).toFixed(2); }

function renderAnalysis(result) {
  if (analysisChart) analysisChart.destroy();
  if (savingChart)   savingChart.destroy();

  const days = (result && result.days) || [];

  const labels     = days.map(d => d.date);

  // Build tariff switch lines from import_periods, only for boundaries within the visible window.
  const periods = (result && result.import_periods) || [];
  const firstDate = labels.length ? labels[0] : null;
  const lastDate  = labels.length ? labels[labels.length - 1] : null;
  const switchLines = periods
    .slice(1)
    .map(p => ({ date: p.from, label: '→ ' + p.tariff }))
    .filter(l => firstDate && lastDate && l.date >= firstDate && l.date <= lastDate);
  // Find the tariff active at the start of the visible window.
  const activePeriod = firstDate && periods.reduce((best, p) =>
    p.from <= firstDate && (!best || p.from > best.from) ? p : best, null);
  const initialTariff = activePeriod ? activePeriod.tariff : null;
  const actualSC = d => includeStandingCharge ? d.actual_standing_charge : 0;
  const cmpSC    = d => includeStandingCharge
    ? (cmpImport === 'agile' ? d.agile_standing_charge : d.go_standing_charge)
    : 0;
  const actualNets = days.map(d => d.actual_net + actualSC(d));
  const cmpNets    = days.map(d => cmpNet(d)    + cmpSC(d));

  const totalActual = actualNets.reduce((a,b) => a+b, 0);
  const totalCmp    = cmpNets.reduce((a,b) => a+b, 0);
  const totalSaving = totalCmp - totalActual; // positive = actual cheaper

  document.getElementById('an-cmp-label').textContent    = cmpLabel();
  document.getElementById('an-saving-label').textContent = 'Saving vs ' + cmpLabel();
  document.getElementById('an-actual').textContent = pence(totalActual);
  document.getElementById('an-cmp').textContent    = pence(totalCmp);
  const savingEl = document.getElementById('an-saving');
  savingEl.textContent = (totalSaving >= 0 ? '+' : '') + pence(totalSaving);
  savingEl.style.color = totalSaving >= 0 ? '#2ecc40' : '#ff4136';

  analysisChart = new Chart(document.getElementById('analysis-chart'), {
    type: 'bar',
    data: {
      labels,
      datasets: [
        {
          label: 'Actual',
          data: actualNets,
          backgroundColor: '#22c55e80',
          borderColor: '#22c55e',
          borderWidth: 1,
        },
        {
          label: cmpLabel(),
          data: cmpNets,
          backgroundColor: '#f59e0b80',
          borderColor: '#f59e0b',
          borderWidth: 1,
        },
      ],
    },
    options: {
      ...chartDefaults,
      plugins: {
        legend: {
          display: true,
          labels: { color: '#9ca3af', font: { family: "'JetBrains Mono','SF Mono',monospace", size: 10 } },
        },
        zoom: zoomConfig,
        tariffSwitches: { lines: switchLines, initial: initialTariff },
        tooltip: { callbacks: { label: ctx => ` ${pence(ctx.parsed.y)}` } },
      },
      scales: {
        x: { ...darkAxis, ticks: { ...darkAxis.ticks, maxTicksLimit: 10 } },
        y: { ...darkAxis, ticks: { ...darkAxis.ticks, callback: v => pence(v) } },
      },
    },
  });

  // cumulative saving: comparison - actual (positive = actual cheaper)
  let cum = 0;
  const cumData = days.map(d => { cum += (cmpNet(d) - d.actual_net); return cum / 100; });
  const lineColor = cum >= 0 ? '#2ecc40' : '#ff4136';

  savingChart = new Chart(document.getElementById('saving-chart'), {
    type: 'line',
    data: {
      labels,
      datasets: [{
        data: cumData,
        borderColor: lineColor,
        backgroundColor: lineColor + '20',
        fill: true,
        pointRadius: 0,
        tension: 0.3,
      }],
    },
    options: {
      ...chartDefaults,
      plugins: {
        legend: { display: false },
        zoom: zoomConfig,
        tariffSwitches: { lines: switchLines, initial: initialTariff },
        tooltip: { callbacks: { label: ctx => ` £${ctx.parsed.y.toFixed(2)}` } },
      },
      scales: {
        x: { ...darkAxis, ticks: { ...darkAxis.ticks, maxTicksLimit: 10 } },
        y: { ...darkAxis, ticks: { ...darkAxis.ticks, callback: v => '£' + v.toFixed(2) } },
      },
    },
  });
  createOverview(analysisChart);
  createOverview(savingChart);
}

function renderBattery(result) {
  if (batteryChart) batteryChart.destroy();
  const switchDate = result.switch_date;
  const days = result.days || [];
  const switchIdx = switchDate ? days.findIndex(d => d.date === switchDate) : -1;

  document.getElementById('bm-date').textContent = switchDate || 'Not detected';

  let preSum = 0, preCount = 0, postSum = 0, postCount = 0;
  days.forEach((d, i) => {
    if (switchIdx < 0 || i < switchIdx) { preSum += d.daytime_charge_kwh; preCount++; }
    else { postSum += d.daytime_charge_kwh; postCount++; }
  });
  document.getElementById('bm-pre').innerHTML =
    preCount ? (preSum / preCount).toFixed(2) + '<span class="stat-unit">kWh</span>' : '—';
  document.getElementById('bm-post').innerHTML =
    postCount ? (postSum / postCount).toFixed(2) + '<span class="stat-unit">kWh</span>' : '—';

  batteryChart = new Chart(document.getElementById('battery-chart'), {
    type: 'bar',
    data: {
      labels: days.map(d => d.date),
      datasets: [{
        data: days.map(d => d.daytime_charge_kwh),
        backgroundColor: days.map((_, i) =>
          switchIdx < 0 || i < switchIdx ? '#3b82f680' : '#22c55e80'),
        borderColor: days.map((_, i) =>
          switchIdx < 0 || i < switchIdx ? '#3b82f6' : '#22c55e'),
        borderWidth: 1,
        barPercentage: 1.0,
        categoryPercentage: 0.9,
      }],
    },
    options: {
      ...chartDefaults,
      plugins: {
        ...chartDefaults.plugins,
        verticalLine: { index: switchIdx },
        zoom: zoomConfig,
        tooltip: { callbacks: { label: ctx => ` ${ctx.parsed.y.toFixed(2)} kWh` } },
      },
      scales: {
        x: { ...darkAxis, ticks: { ...darkAxis.ticks, maxTicksLimit: 12 } },
        y: { ...darkAxis, ticks: { ...darkAxis.ticks, callback: v => v.toFixed(1) } },
      },
    },
  });
  createOverview(batteryChart);
}

function renderCharging(result) {
  if (chargingChart) chargingChart.destroy();
  const days = result.days || [];
  const breakeven = result.breakeven_yield_kwh || 0;
  const seasons = result.seasons || {};

  const depletedCount = days.filter(d => d.depleted).length;
  document.getElementById('co-threshold').innerHTML =
    breakeven.toFixed(1) + '<span class="stat-unit">kWh</span>';
  document.getElementById('co-depleted').textContent =
    `${depletedCount} / ${days.length}`;

  chargingChart = new Chart(document.getElementById('charging-chart'), {
    type: 'bar',
    data: {
      labels: days.map(d => d.date),
      datasets: [
        {
          type: 'bar',
          label: 'Solar yield',
          data: days.map(d => d.total_yield_kwh),
          backgroundColor: days.map(d => d.depleted ? '#ef444466' : '#22c55e40'),
          borderColor:     days.map(d => d.depleted ? '#ef4444'   : '#22c55e'),
          borderWidth: 1,
          yAxisID: 'y',
          barPercentage: 1.0,
          categoryPercentage: 0.9,
        },
        {
          type: 'line',
          label: 'Min SoC',
          data: days.map(d => d.min_soc),
          borderColor: '#3b82f6',
          backgroundColor: 'transparent',
          pointRadius: 0,
          tension: 0.3,
          yAxisID: 'y1',
        },
        {
          type: 'line',
          label: '15% threshold',
          data: days.map(() => 15),
          borderColor: '#f59e0b80',
          borderDash: [4, 4],
          borderWidth: 1.5,
          pointRadius: 0,
          yAxisID: 'y1',
        },
      ],
    },
    options: {
      ...chartDefaults,
      plugins: {
        legend: {
          display: true,
          labels: {
            color: '#9ca3af',
            font: { family: "'JetBrains Mono','SF Mono',monospace", size: 10 },
            filter: item => item.text !== '15% threshold',
          },
        },
        zoom: zoomConfig,
        tooltip: {
          callbacks: {
            label: ctx => {
              if (ctx.dataset.label === '15% threshold') return null;
              if (ctx.dataset.label === 'Min SoC') return ` min SoC: ${ctx.parsed.y.toFixed(0)}%`;
              return ` yield: ${ctx.parsed.y.toFixed(1)} kWh`;
            },
          },
        },
      },
      scales: {
        x: { ...darkAxis, ticks: { ...darkAxis.ticks, maxTicksLimit: 12 } },
        y: {
          ...darkAxis,
          position: 'left',
          title: { display: true, text: 'Solar yield (kWh)', color: '#6b7280',
            font: { family: "'JetBrains Mono','SF Mono',monospace", size: 10 } },
          ticks: { ...darkAxis.ticks, callback: v => v + ' kWh' },
        },
        y1: {
          ...darkAxis,
          position: 'right',
          min: 0, max: 100,
          grid: { drawOnChartArea: false },
          title: { display: true, text: 'Min SoC (%)', color: '#6b7280',
            font: { family: "'JetBrains Mono','SF Mono',monospace", size: 10 } },
          ticks: { ...darkAxis.ticks, callback: v => v + '%' },
        },
      },
    },
  });
  createOverview(chargingChart);

  const seasonOrder = ['spring', 'summer', 'autumn', 'winter'];
  const table = document.getElementById('co-seasons-table');
  table.innerHTML =
    `<tr style="color:#4b5563;border-bottom:1px solid #1e1e2e">
       <th style="text-align:left;padding:8px 4px">Season</th>
       <th style="text-align:right;padding:8px 4px">Threshold</th>
       <th style="text-align:right;padding:8px 4px">Depleted days</th>
     </tr>` +
    seasonOrder.map(s => {
      const st = seasons[s];
      if (!st) return '';
      const pct = st.total_days ? Math.round(100 * st.depleted_days / st.total_days) : 0;
      return `<tr style="border-bottom:1px solid #1e1e2e">
        <td style="padding:6px 4px;text-transform:capitalize;color:#d1d5db">${s}</td>
        <td style="text-align:right;padding:6px 4px;color:#a78bfa">${st.breakeven_yield_kwh.toFixed(1)} kWh</td>
        <td style="text-align:right;padding:6px 4px;color:#9ca3af">${st.depleted_days} / ${st.total_days} (${pct}%)</td>
      </tr>`;
    }).join('');
}

function renderSmartCharging(result) {
  if (smartChart) smartChart.destroy();
  if (smartCumulativeChart) smartCumulativeChart.destroy();
  
  const summary = result && result.summary || {};
  const days = (result && result.days) || [];

  document.getElementById('sc-days-savings').textContent = 
    `${summary.days_with_savings || 0} / ${summary.total_days || 0}`;
  
  const savings = (summary.total_net_savings_pence || 0) / 100;
  const savingsEl = document.getElementById('sc-savings');
  savingsEl.textContent = (savings >= 0 ? '+' : '') + '£' + savings.toFixed(2);
  savingsEl.style.color = savings >= 0 ? '#2ecc40' : '#ff4136';
  
  const avgSavings = (summary.avg_savings_per_day_pence || 0) / 100;
  document.getElementById('sc-avg-savings').innerHTML = 
    '£' + avgSavings.toFixed(2) + '<span class="stat-unit">/day</span>';
  
  document.getElementById('sc-cycles').innerHTML = 
    (summary.total_cycles_used || 0).toFixed(1) + '<span class="stat-unit">cycles</span>';

  const labels = days.map(d => d.date);
  
  // Cost comparison chart
  smartChart = new Chart(document.getElementById('smart-chart'), {
    type: 'bar',
    data: {
      labels,
      datasets: [
        {
          label: 'Baseline',
          data: days.map(d => d.baseline_cost_pence / 100),
          backgroundColor: '#ff851b40',
          borderColor: '#ff851b',
          borderWidth: 1,
        },
        {
          label: 'Optimized',
          data: days.map(d => d.optimized_cost_pence / 100),
          backgroundColor: '#22c55e40',
          borderColor: '#22c55e',
          borderWidth: 1,
        },
      ],
    },
    options: {
      ...chartDefaults,
      plugins: {
        legend: {
          display: true,
          labels: { color: '#9ca3af', font: { family: "'JetBrains Mono','SF Mono',monospace", size: 10 } },
        },
        zoom: zoomConfig,
        tooltip: { callbacks: { label: ctx => ` £${ctx.parsed.y.toFixed(2)}` } },
      },
      scales: {
        x: { ...darkAxis, ticks: { ...darkAxis.ticks, maxTicksLimit: 10 } },
        y: { ...darkAxis, ticks: { ...darkAxis.ticks, callback: v => '£' + v.toFixed(2) } },
      },
    },
  });

  // Cumulative savings chart
  let cum = 0;
  const cumData = days.map(d => {
    cum += (d.net_savings_pence || 0) / 100;
    return cum;
  });
  const cumColor = cum >= 0 ? '#22c55e' : '#ef4444';

  smartCumulativeChart = new Chart(document.getElementById('smart-cumulative-chart'), {
    type: 'line',
    data: {
      labels,
      datasets: [{
        data: cumData,
        borderColor: cumColor,
        backgroundColor: cumColor + '20',
        fill: true,
        pointRadius: 0,
        tension: 0.3,
      }],
    },
    options: {
      ...chartDefaults,
      plugins: {
        legend: { display: false },
        zoom: zoomConfig,
        tooltip: { callbacks: { label: ctx => ` £${ctx.parsed.y.toFixed(2)}` } },
      },
      scales: {
        x: { ...darkAxis, ticks: { ...darkAxis.ticks, maxTicksLimit: 10 } },
        y: { ...darkAxis, ticks: { ...darkAxis.ticks, callback: v => '£' + v.toFixed(2) } },
      },
    },
  });

  createOverview(smartChart);
  createOverview(smartCumulativeChart);
}

async function loadSmartCharging() {
  const from = fmtDate(rangeStart);
  const to = fmtDate(rangeEnd());
  const url = `/api/charging/optimization?region=${currentRegion}&from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`;
  setLoading(true);
  try {
    const res = await fetch(url);
    if (!res.ok) {
      const text = await res.text();
      console.error('Smart charging fetch failed:', res.status, text);
      return;
    }
    const text = await res.text();
    lastSmartChargingData = JSON.parse(text);
    renderSmartCharging(lastSmartChargingData);
    smartChargingLoaded = true;
  } catch (err) {
    console.error('Smart charging load error:', err);
  } finally {
    setLoading(false);
  }
}

async function loadCharging() {
  if (chargingLoaded) return;
  setLoading(true);
  try {
    const res = await fetch('/api/battery/charging-optimisation');
    if (!res.ok) return;
    renderCharging(await res.json());
    chargingLoaded = true;
  } finally {
    setLoading(false);
  }
}

async function loadBattery() {
  if (batteryLoaded) return;
  setLoading(true);
  try {
    const res = await fetch('/api/battery/mode-switch');
    if (!res.ok) return;
    renderBattery(await res.json());
    batteryLoaded = true;
  } finally {
    setLoading(false);
  }
}

async function loadAnalysis() {
  const qs = `region=${currentRegion}&from=${fmtDate(rangeStart)}&to=${fmtDate(rangeEnd())}`;
  setLoading(true);
  try {
    const res = await fetch(`/api/analysis?${qs}`);
    if (!res.ok) return;
    lastAnalysisData = await res.json();
    renderAnalysis(lastAnalysisData || {});
  } finally {
    setLoading(false);
  }
}

// ── event wiring ─────────────────────────────────────────────────────────
function spanDays() {
  return windowDays === 1 ? 1 : Math.round((rangeEndDate - rangeStart) / 86400000) + 1;
}

function shiftRange(days) {
  rangeStart   = new Date(rangeStart);
  rangeEndDate = new Date(rangeEndDate);
  rangeStart.setUTCDate(rangeStart.getUTCDate()     + days);
  rangeEndDate.setUTCDate(rangeEndDate.getUTCDate() + days);
  loadData();
}

document.getElementById('prev').addEventListener('click', () => shiftRange(-spanDays()));
document.getElementById('next').addEventListener('click', () => shiftRange(spanDays()));

document.getElementById('range-from').addEventListener('change', function() {
  const d = new Date(this.value + 'T00:00:00Z');
  if (!isNaN(d)) { rangeStart = d; if (rangeStart <= rangeEndDate) loadData(); }
});
document.getElementById('range-to').addEventListener('change', function() {
  const d = new Date(this.value + 'T00:00:00Z');
  if (!isNaN(d)) { rangeEndDate = d; if (rangeStart <= rangeEndDate) loadData(); }
});

function setActiveWindowTab(btn) {
  document.querySelectorAll('.window-tab').forEach(b => b.classList.toggle('active', b === btn));
}

document.querySelectorAll('.window-tab[data-days]').forEach(btn =>
  btn.addEventListener('click', () => {
    windowDays = parseInt(btn.dataset.days, 10);
    const t = today();
    rangeEndDate = new Date(t);
    if (windowDays === 1) rangeEndDate.setUTCDate(rangeEndDate.getUTCDate() + 1);
    rangeStart   = new Date(t);
    rangeStart.setUTCDate(t.getUTCDate() - (windowDays - 1));
    setActiveWindowTab(btn);
    loadData();
  }));

document.querySelector('.window-tab[data-mode="mtd"]').addEventListener('click', function() {
  const t = today();
  rangeStart   = new Date(Date.UTC(t.getUTCFullYear(), t.getUTCMonth(), 1));
  rangeEndDate = new Date(t);
  setActiveWindowTab(this);
  loadData();
});

document.querySelector('.window-tab[data-mode="ytd"]').addEventListener('click', function() {
  const t = today();
  rangeStart   = new Date(Date.UTC(t.getUTCFullYear(), 0, 1));
  rangeEndDate = new Date(t);
  setActiveWindowTab(this);
  loadData();
});

document.querySelectorAll('.tab').forEach(btn =>
  btn.addEventListener('click', () => {
    switchView(btn.dataset.view);
    if (btn.dataset.view === 'analysis') loadAnalysis();
    if (btn.dataset.view === 'battery')  loadBattery();
    if (btn.dataset.view === 'charging') loadCharging();
    if (btn.dataset.view === 'smart') loadSmartCharging();
  }));
document.querySelectorAll('.scenario-tab[data-stype]').forEach(btn =>
  btn.addEventListener('click', () => {
    const stype = btn.dataset.stype;
    if (stype === 'import') cmpImport = btn.dataset.sval;
    else cmpExport = btn.dataset.sval;
    document.querySelectorAll(`.scenario-tab[data-stype="${stype}"]`).forEach(b =>
      b.classList.toggle('active', b === btn));
    if (lastAnalysisData) renderAnalysis(lastAnalysisData);
  }));
document.getElementById('standing-charge-toggle').addEventListener('click', function() {
  includeStandingCharge = !includeStandingCharge;
  this.classList.toggle('active', includeStandingCharge);
  if (lastAnalysisData) renderAnalysis(lastAnalysisData);
});

document.getElementById('region-select').addEventListener('change', function() {
  currentRegion = this.value;
  loadData();
});

// Initialise: fetch default region from server config, then load data.
(async function init() {
  try {
    const res = await fetch('/api/config');
    if (res.ok) {
      const cfg = await res.json();
      if (cfg.region) {
        currentRegion = cfg.region;
        const sel = document.getElementById('region-select');
        if (sel) sel.value = currentRegion;
      }
    }
  } catch (e) {
    console.warn('Could not fetch /api/config, using default region', e);
  }
  loadData();
})();
  

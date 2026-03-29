# Solar + Octopus Agile Analysis Project

## Project Brief for Claude Code

### Goal

Build a system to harvest historical solar/battery data from SolaX Cloud, store it durably in S3-compatible object storage, and provide a Go-based analysis app that compares actual energy costs (on Octopus Go) against what costs would have been on Octopus Agile.

---

## System Context

### Hardware

- **Solar panels** with battery (12 kWh), managed by a **SolaX inverter**
- Installed **April 2025**
- Monitored via **SolaX Cloud** (solaxcloud.com)
- At some point (likely autumn 2025), battery mode was switched from **export priority** to **self-use/battery priority** for winter — this is not explicitly recorded in the data and should be inferred from the data patterns

### Current Tariff: Octopus Go

- **5-hour off-peak window** (check exact times — likely 00:30–05:30 or 23:30–04:30)
- Standard peak rate for remaining hours
- **Variable export tariff** (Octopus Outgoing) — currently 12p/kWh, was previously 15p/kWh
- Standing charge applies

### Metering

- **SMETS2 smart meter** with half-hourly consumption data available via the Octopus API
- SolaX Cloud records data at **5-minute intervals**

### Location

- Gloucestershire, UK
- DNO region needs confirming from MPAN — likely **Western Power Distribution (region P or similar)**

---

## Phase 0: SolaX Cloud API Discovery

### Objective

Reverse-engineer the SolaX Cloud app's API to find historical data endpoints, since the official end-user API only exposes real-time data.

### Approach

1. Set up **mitmproxy** (or Charles Proxy) as an HTTPS proxy
2. Install the mitmproxy CA certificate on phone
3. Open the SolaX Cloud app and navigate through:
   - Daily detail views (5-minute resolution generation/consumption/battery graphs)
   - Monthly and yearly summary views
   - Battery analysis screens (SoC, charge/discharge)
   - Any export/import breakdown views
4. Capture and document:
   - Base URL and endpoint paths
   - Authentication mechanism (token, cookie, etc.)
   - Request parameters (date ranges, device IDs, granularity)
   - Response schemas (JSON structure, field names, units)
   - Pagination patterns (if any)
5. Test whether historical data goes back to April 2025

### Key Data Points to Capture

- **Solar generation** (PV output in kW/kWh)
- **Grid import** (kW/kWh)
- **Grid export** (kW/kWh)
- **Battery charge** (kW/kWh)
- **Battery discharge** (kW/kWh)
- **Battery SoC** (state of charge, %)
- **Self-consumption** (kW/kWh)
- **Load/consumption** (total household demand)

### Deliverable

A document describing the discovered API endpoints, auth flow, and response schemas — sufficient to write an automated data harvester.

### Fallback

If the app uses certificate pinning or the API is otherwise inaccessible:
- Use the **manual CSV export** from the SolaX Cloud web UI (Data Report feature) to export whatever is available for the full April 2025–present period
- The web export may only include Yield, Feed-In Energy, and Consume Energy — battery detail may be missing
- Consider the **local inverter API** (Modbus TCP or local webserver on the WiFi dongle) as a richer ongoing data source

---

## Phase 1: Historical Data Harvest

### Objective

Bulk export all historical data from SolaX Cloud into S3-compatible object storage.

### Storage Design

**Provider:** S3-compatible (Cloudflare R2 or Storj — code should use the S3 API and be provider-agnostic)

**Bucket structure:**

```
solar-data/
├── solax/
│   ├── raw/                          # Raw API responses, archived as-is
│   │   ├── 2025/04/15/
│   │   │   ├── daily-detail.json     # 5-min resolution data for the day
│   │   │   └── battery-detail.json   # Battery-specific data if separate endpoint
│   │   └── ...
│   └── processed/                    # Normalised, query-friendly format
│       ├── 2025/04/15.parquet        # Or .json — one file per day, consistent schema
│       └── ...
├── octopus/
│   ├── agile-import/                 # Half-hourly Agile import rates
│   │   ├── 2025/04.json
│   │   └── ...
│   ├── agile-export/                 # Half-hourly Agile Outgoing rates
│   │   ├── 2025/04.json
│   │   └── ...
│   ├── go-rates/                     # Octopus Go rate history (for baseline comparison)
│   │   └── rates.json                # Peak/off-peak rates over time
│   └── consumption/                  # SMETS2 half-hourly import/export from Octopus API
│       ├── 2025/04.json
│       └── ...
└── analysis/                         # Derived/computed outputs
    └── ...
```

**Design notes:**
- Store raw API responses verbatim in `raw/` — never modify these, they're the archive
- `processed/` contains normalised data with a consistent schema, derived from raw
- Parquet is preferred for processed data (efficient, columnar, works with DuckDB) but JSON is acceptable for phase 1
- One file per day keeps objects small and makes incremental updates simple
- Keep Octopus data separate — different source, different cadence

### Implementation

Write a Go CLI tool that:
1. Authenticates with SolaX Cloud using the discovered API
2. Iterates day-by-day from April 2025 to present
3. Fetches all available data for each day
4. Stores raw responses in the `raw/` prefix
5. Transforms into normalised schema and stores in `processed/`
6. Respects rate limits (the official API allows 10 req/min, 10k/day — the undocumented endpoints may have different limits, so be conservative)
7. Is idempotent — can be re-run safely, skipping days already harvested
8. Logs progress and any gaps/failures

---

## Phase 2: Ongoing Data Collection

### Objective

Set up continuous data collection so the archive stays current after the initial bulk harvest.

### Approach

A Go service (or cron job) that runs every 5 minutes:
1. Polls the SolaX Cloud API (or local inverter API) for the latest data point
2. Appends to the current day's raw file in S3
3. At end of day (or next morning), generates the processed file for the previous day

### Local Inverter Option

The SolaX inverter exposes a local API via the WiFi dongle. This can serve as:
- A **primary source** for ongoing collection (lower latency, no cloud dependency)
- A **fallback** if the cloud API changes or becomes unavailable
- A source of **richer data** (Modbus TCP can expose ~240 entities)

The WiFi dongle password is available under "Backup password" in SolaX Cloud settings.

---

## Phase 3: Octopus Data Integration

### Objective

Download Agile pricing history and actual consumption data from the Octopus API to enable the cost comparison.

### Octopus API Details

**Base URL:** `https://api.octopus.energy/v1/`

**Agile import rates (no auth required):**
```
GET /products/AGILE-FLEX-22-11-25/electricity-tariffs/E-1R-AGILE-FLEX-22-11-25-{REGION}/standard-unit-rates/
?period_from=2025-04-01T00:00Z
&period_to=2025-04-02T00:00Z
```

- Product code may vary — check the current active Agile product code
- Region code derived from MPAN (e.g., `P` for Western Power Distribution)
- Returns half-hourly rates in p/kWh including VAT
- Paginated — follow `next` links

**Agile export rates (no auth required):**
```
GET /products/AGILE-OUTGOING-19-05-13/electricity-tariffs/E-1R-AGILE-OUTGOING-19-05-13-{REGION}/standard-unit-rates/
?period_from=...&period_to=...
```

**Consumption data (auth required):**
```
GET /electricity-meter-points/{MPAN}/meters/{SERIAL}/consumption/
?period_from=...&period_to=...&order_by=period
```

- Requires API key (available from Octopus account dashboard)
- Returns half-hourly kWh readings from SMETS2 meter
- Separate endpoints for import and export meters

**Rate notes:**
- Always use UTC/Zulu format for dates (append `Z`)
- Half-hourly periods: `valid_from` and `valid_to` define each slot
- Consumption data may have gaps — SMETS2 data delivery is sometimes delayed

### Implementation

Extend the Go CLI to:
1. Fetch and store Agile import rates for the full period (April 2025–present)
2. Fetch and store Agile export rates for the same period
3. Fetch and store actual half-hourly consumption (import + export) from Octopus
4. Record current Octopus Go rates (peak, off-peak, standing charge) with date ranges for when they changed
5. Store in the `octopus/` prefix in S3

---

## Phase 4: Analysis App

### Objective

A Go backend with a simple web frontend that visualises the data and answers the core question: **"Would I have been better off on Octopus Agile?"**

### Architecture

```
┌─────────────────────┐
│   Browser (SPA)     │  Simple frontend — vanilla JS, htmx, or lightweight React
└──────┬──────────────┘
       │ HTTP/JSON
┌──────▼──────────────┐
│   Go HTTP Server    │  Serves API + static frontend
│                     │
│  ┌───────────────┐  │
│  │  DuckDB or    │  │  Query engine over Parquet/JSON in S3
│  │  in-memory    │  │  (DuckDB can query S3 directly)
│  └───────────────┘  │
└──────┬──────────────┘
       │ S3 API
┌──────▼──────────────┐
│  Object Storage     │  Cloudflare R2 / Storj
│  (S3-compatible)    │
└─────────────────────┘
```

### Core Views

#### 1. Dashboard / Overview
- Daily/weekly/monthly generation, consumption, import, export, self-consumption ratio
- Battery SoC timeline
- Current period cost summary

#### 2. Solar & Battery Detail
- 5-minute resolution charts for any given day
- Generation vs consumption overlay
- Battery charge/discharge patterns
- **Mode switch detection:** identify the date when the export-to-charge ratio shifted (export priority → self-use), highlight it on the timeline

#### 3. Agile Cost Comparison (the main event)

**What you actually paid (Octopus Go):**
- Calculate from SMETS2 half-hourly import data × Go rate (off-peak or peak depending on time slot)
- Subtract export income (half-hourly export × Outgoing variable rate)
- Add standing charge

**What you would have paid (Octopus Agile):**
- Same half-hourly import volumes × Agile import rate for that slot
- Same half-hourly export volumes × Agile Outgoing rate for that slot
- Add Agile standing charge
- Note: this is the "naive" comparison — same behaviour, different tariff

**What you could have paid (Agile + optimised battery):**
- Model optimal battery behaviour under Agile pricing:
  - Charge from grid when Agile price is very low or negative
  - Discharge battery during peak pricing (16:00–19:00 typically)
  - Still prioritise solar self-consumption
- This is a more complex simulation but shows the theoretical upside
- Constraints: battery capacity (12 kWh), charge/discharge rate limits, round-trip efficiency losses (~90%)

**Output:**
- Monthly and annual cost comparison table
- Running total: cumulative savings or losses vs Agile
- Highlight periods where Agile would have been significantly cheaper or more expensive
- Show the impact of plunge pricing events (negative rates) on the Agile scenario

#### 4. Export Analysis
- Compare Outgoing variable rate vs Agile Outgoing rates
- Show how export income would differ
- Important for summer months when export is high

#### 5. Charging Priority Optimisation

The inverter has two relevant modes:
- **Grid priority:** charge the battery from cheap off-peak grid electricity (Octopus Go off-peak window)
- **Solar/self-use priority:** charge the battery from solar generation, minimising grid interaction

The optimal mode on any given day depends on a trade-off: paying a known off-peak rate now vs relying on solar generation that may or may not materialise. This view answers: **"Given the off-peak rate and the day's solar output, when was it better to have charged from the grid instead of waiting for solar — and what's the decision rule?"**

**Historical analysis:**
- For each day in the dataset, calculate the outcome under each mode:
  - Grid priority: battery charged during off-peak window at Go off-peak rate; solar generation used for self-consumption/export as normal
  - Solar priority: battery charged from solar only; any shortfall met from grid at peak rate
- Compare the actual daily cost under each mode
- Identify which mode was optimal and by how much

**Decision boundary:**
- Plot the outcome difference (grid priority cost − solar priority cost) against actual solar yield for that day
- Identify the solar yield threshold below which grid charging was cheaper — this is the switching point
- Check whether this threshold is stable or varies by season (it will — winter days have low solar ceilings, summer days often exceed battery capacity)
- Express as a simple rule: e.g. "charge from grid if forecast solar yield < N kWh"

**Output:**
- Scatter plot: daily solar yield vs cost difference between modes, with decision boundary marked
- Seasonal breakdown: separate thresholds for winter/spring/summer/autumn
- Summary of how many days the "wrong" mode was active (based on inferred mode switch date) and what it cost
- If on Agile: recalculate with variable off-peak rates — the threshold becomes dynamic (cheap grid slots shift the boundary)

#### 6. Tariff Explorer (stretch goal)
- Compare against other Octopus tariffs (Tracker, Cosy, Intelligent Go)
- The Octopus API exposes rates for all products, so this is feasible

### API Endpoints (Go backend)

```
GET /api/solar/daily?date=2025-04-15          # 5-min resolution solar/battery data
GET /api/solar/summary?from=...&to=...        # Aggregated generation/consumption
GET /api/comparison/monthly?month=2025-04     # Go vs Agile cost breakdown
GET /api/comparison/cumulative?from=...&to=...# Running total comparison
GET /api/agile/rates?date=2025-04-15          # Agile rates for a given day
GET /api/battery/mode-switch                  # Detected mode switch date + evidence
GET /api/export/analysis?from=...&to=...      # Export tariff comparison
GET /api/battery/charging-optimisation?from=...&to=... # Grid vs solar priority analysis + decision boundary
```

---

## Technical Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Language | Go | Existing expertise, good for CLI tools and HTTP servers |
| Storage | S3-compatible (R2/Storj) | Durable, cheap, portable, no DB to manage |
| Data format | Parquet (processed), JSON (raw) | Parquet is efficient for analytics; raw JSON preserves original responses |
| Query layer | DuckDB (via Go bindings) | Can query Parquet files in S3 directly, no ETL pipeline needed |
| Frontend | Minimal — htmx or vanilla JS with a charting lib | Keep it simple, charts are the main UI element |
| Charting | Chart.js, Plotly, or similar | Needs good time-series support with zoom/pan |
| Config | Environment variables + YAML config file | S3 credentials, Octopus API key, SolaX token, region codes |

---

## Configuration Required

The user will need to provide:

```yaml
solax:
  token_id: "..."           # From SolaX Cloud → Service → API page
  inverter_sn: "..."        # Module serial number
  # Plus any discovered auth details from Phase 0

octopus:
  api_key: "..."            # From Octopus account dashboard
  mpan_import: "..."        # Import MPAN
  mpan_export: "..."        # Export MPAN
  meter_serial_import: "..."
  meter_serial_export: "..."
  region: "P"               # DNO region code (confirm from MPAN)
  go_rates:                 # Historical Go rates
    - from: "2025-04-01"
      peak_rate: 24.50      # p/kWh — example, confirm actual
      offpeak_rate: 7.50    # p/kWh — example, confirm actual
      offpeak_start: "00:30"
      offpeak_end: "05:30"
      standing_charge: 46.36 # p/day — example
  export_rates:
    - from: "2025-04-01"
      rate: 15.0            # p/kWh
    - from: "2025-10-01"    # approximate — when it dropped
      rate: 12.0

s3:
  endpoint: "..."           # R2/Storj endpoint
  bucket: "solar-data"
  access_key: "..."
  secret_key: "..."
  region: "auto"            # R2 uses "auto"
```

---

## Risks and Considerations

1. **SolaX app may use certificate pinning** — if mitmproxy can't intercept, fall back to manual CSV export + local inverter API for ongoing collection
2. **SolaX Cloud data retention** — unclear how long historical data is kept; the bulk harvest should happen soon
3. **Octopus consumption data gaps** — SMETS2 data delivery is unreliable; expect some missing half-hours that need interpolation or flagging
4. **Agile product codes change** — the active Agile product code rotates periodically; the harvester should discover the current code dynamically via `/v1/products/`
5. **Battery optimisation model is an approximation** — real-world battery behaviour includes charge rate curves, temperature effects, and inverter efficiency losses; keep the model simple initially
6. **The "naive" Agile comparison assumes identical behaviour** — in practice, being on Agile would change behaviour (shifting loads to cheap periods), so the comparison is a lower bound on Agile savings
7. **Export rate history** — the Octopus Outgoing variable rate changes; you'll need to record the rate change dates accurately for the baseline calculation
8. **Go rate changes** — if the Go peak/off-peak rates changed during the period, record all historical rates with date ranges

---

## Suggested Build Order

1. **Phase 0** — API discovery with mitmproxy (manual, 1-2 hours)
2. **Phase 1** — Go CLI: bulk harvest SolaX data → S3
3. **Phase 3 (partial)** — Go CLI: download Octopus Agile rates + consumption → S3
4. **Phase 4 (core)** — Go backend: Agile comparison calculations, served as JSON API
5. **Phase 4 (frontend)** — Simple web UI with charts
6. **Phase 2** — Ongoing collection cron job
7. **Phase 4 (advanced)** — Battery optimisation model, tariff explorer
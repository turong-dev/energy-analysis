package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"energy-utility/internal/analysis"
	"energy-utility/internal/analysis/battery"
	"energy-utility/internal/config"
	"energy-utility/internal/device"
	"energy-utility/internal/device/solax"
	"energy-utility/internal/store"
	"energy-utility/internal/tariff"
	"energy-utility/internal/tariff/octopus"
)

type dataPoint struct {
	T string  `json:"t"`
	V float64 `json:"v"`
}

type demoData struct {
	RatesImport       []dataPoint                          `json:"rates-import"`
	RatesExport       []dataPoint                          `json:"rates-export"`
	ConsumptionImport []dataPoint                          `json:"consumption-import"`
	ConsumptionExport []dataPoint                          `json:"consumption-export"`
	Analysis          analysis.AnalysisResult              `json:"analysis"`
	ModeSwitch        analysis.ModeSwitchResult            `json:"battery-mode-switch"`
	Charging          analysis.ChargingOptResult           `json:"battery-charging"`
	SmartCharging     battery.ChargingOptimizationResponse `json:"smart-charging"`
}

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config file")
	fromStr := flag.String("from", "", "start date YYYY-MM-DD (default: 30 days ago)")
	toStr := flag.String("to", "", "end date YYYY-MM-DD inclusive (default: today)")
	outDir := flag.String("out", "demo", "output directory")
	uiSrc := flag.String("ui", "cmd/serve/ui/index.html", "path to source index.html")
	cssSrc := flag.String("css", "", "path to source styles.css (default: sibling of uiSrc)")
	jsSrc := flag.String("js", "", "path to source app.js (default: sibling of uiSrc)")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx := context.Background()

	s3raw, err := store.New(ctx, cfg.S3)
	if err != nil {
		log.Fatalf("init s3: %v", err)
	}
	var s3 store.Store = s3raw
	if cfg.CacheDir != "" {
		s3 = store.NewCached(s3raw, cfg.CacheDir)
	}

	from, to := parseDateRange(*fromStr, *toStr)
	log.Printf("snapshot range: %s – %s", from.Format("2006-01-02"), to.Add(-time.Second).Format("2006-01-02"))

	var d demoData

	region := cfg.Octopus.Region
	if region == "" {
		region = "E"
	}

	for _, dir := range []string{"import", "export"} {
		rates, err := octopus.ReadRates(ctx, s3, dir, region, from, to)
		if err != nil {
			log.Fatalf("rates %s: %v", dir, err)
		}
		points := make([]dataPoint, len(rates))
		for i, r := range rates {
			points[i] = dataPoint{T: r.ValidFrom.UTC().Format(time.RFC3339), V: r.ValueIncVAT}
		}
		sort.Slice(points, func(i, j int) bool { return points[i].T < points[j].T })
		if dir == "import" {
			d.RatesImport = points
		} else {
			d.RatesExport = points
		}
		log.Printf("fetched rates-%s (%d points)", dir, len(points))

		readings, err := octopus.ReadConsumption(ctx, s3, dir, from, to)
		if err != nil {
			log.Fatalf("consumption %s: %v", dir, err)
		}
		cpoints := make([]dataPoint, len(readings))
		for i, c := range readings {
			cpoints[i] = dataPoint{T: c.IntervalStart.UTC().Format(time.RFC3339), V: c.Consumption}
		}
		sort.Slice(cpoints, func(i, j int) bool { return cpoints[i].T < cpoints[j].T })
		if dir == "import" {
			d.ConsumptionImport = cpoints
		} else {
			d.ConsumptionExport = cpoints
		}
		log.Printf("fetched consumption-%s (%d points)", dir, len(readings))
	}

	importRates, err := octopus.ReadRates(ctx, s3, "import", region, from, to)
	if err != nil {
		log.Fatalf("analysis import rates: %v", err)
	}
	exportRates, err := octopus.ReadRates(ctx, s3, "export", region, from, to)
	if err != nil {
		log.Fatalf("analysis export rates: %v", err)
	}
	importCons, err := octopus.ReadConsumption(ctx, s3, "import", from, to)
	if err != nil {
		log.Fatalf("analysis import consumption: %v", err)
	}
	exportCons, err := octopus.ReadConsumption(ctx, s3, "export", from, to)
	if err != nil {
		log.Fatalf("analysis export consumption: %v", err)
	}

	var importAgreements, exportAgreements []octopus.TariffAgreement
	if cfg.Octopus.AccountID != "" {
		oc := octopus.NewClient(cfg.Octopus.APIKey)
		imp, exp, aerr := oc.FetchAgreements(ctx, cfg.Octopus.AccountID)
		if aerr != nil {
			log.Printf("warn: fetch agreements: %v (falling back to Go tariff)", aerr)
		} else {
			importAgreements = imp
			exportAgreements = exp
			log.Printf("fetched agreements: %d import, %d export", len(imp), len(exp))
		}
	}

	d.Analysis = analysis.Calculate(toTariffRates(importRates), toTariffRates(exportRates), importCons, exportCons, importAgreements, exportAgreements, &cfg.Octopus, 0)
	log.Printf("fetched analysis (%d days)", len(d.Analysis.Days))

	solaxDays, err := solax.ReadDays(ctx, s3)
	if err != nil {
		log.Fatalf("read solax days: %v", err)
	}
	deviceDays := toDeviceDays(solaxDays)
	var filteredDeviceDays []device.DayData
	for _, dd := range deviceDays {
		dateStr := dd.Date.Format("2006-01-02")
		if dateStr < from.Format("2006-01-02") || dateStr >= to.Format("2006-01-02") {
			continue
		}
		filteredDeviceDays = append(filteredDeviceDays, dd)
	}
	d.ModeSwitch = analysis.DetectModeSwitch(filteredDeviceDays)
	d.Charging = analysis.AnalyseCharging(filteredDeviceDays)
	log.Printf("fetched battery data (%d solax days, %d in range)", len(solaxDays), len(filteredDeviceDays))

	// Generate smart-charging simulation.
	batteryCfg := cfg.Battery.ToDeviceConfig()
	if batteryCfg.CapacityKWh == 0 {
		batteryCfg = device.DefaultBatteryConfig()
	}
	var simDays []battery.DaySimulationResult
	for _, sd := range solaxDays {
		date, err := time.Parse("2006-01-02", sd.Date)
		if err != nil {
			continue
		}
		if date.Before(from) || !date.Before(to) {
			continue
		}
		solarHH, loadHH := battery.AggregateToHalfHour(sd.PVPower, sd.LoadPower)
		if len(solarHH) != 48 || len(loadHH) != 48 {
			continue
		}
		dayRates := filterRatesForDay(importRates, date)
		dayExport := filterRatesForDay(exportRates, date)
		if len(dayRates) != 48 {
			continue
		}
		input := battery.DaySimulationInput{
			Date:        sd.Date,
			ImportRates: dayRates,
			ExportRates: dayExport,
			SolarPower:  solarHH,
			LoadPower:   loadHH,
			InitialSoC:  50,
			Config:      batteryCfg,
		}
		simDays = append(simDays, battery.SimulateDay(input))
	}
	d.SmartCharging = battery.ChargingOptimizationResponse{
		Days:    simDays,
		Summary: battery.SummarizeResults(simDays),
	}
	log.Printf("fetched smart charging (%d simulated days)", len(simDays))

	dataJSON, err := json.Marshal(d)
	if err != nil {
		log.Fatalf("marshal demo data: %v", err)
	}

	htmlBytes, err := os.ReadFile(*uiSrc)
	if err != nil {
		log.Fatalf("read %s: %v", *uiSrc, err)
	}
	html := string(htmlBytes)

	cssPath := *cssSrc
	if cssPath == "" {
		cssPath = filepath.Join(filepath.Dir(*uiSrc), "styles.css")
	}
	cssBytes, err := os.ReadFile(cssPath)
	if err != nil {
		log.Fatalf("read %s: %v", cssPath, err)
	}
	css := string(cssBytes)
	// Hide date controls in the demo snapshot.
	css += "\n#nav-arrows, .window-tabs, #region-select { display: none !important; }\n"
	html = strings.Replace(html, `<link rel="stylesheet" href="styles.css">`, "<style>\n"+css+"\n</style>", 1)

	jsPath := *jsSrc
	if jsPath == "" {
		jsPath = filepath.Join(filepath.Dir(*uiSrc), "app.js")
	}
	jsBytes, err := os.ReadFile(jsPath)
	if err != nil {
		log.Fatalf("read %s: %v", jsPath, err)
	}
	js := string(jsBytes)

	// Lock demo to the full snapshot range and 30-day window.
	js = strings.Replace(js, "let windowDays = 7;", "let windowDays = 30;", 1)
	js = strings.Replace(js, `let rangeStart = (() => {
  const d = new Date();
  d.setUTCHours(0, 0, 0, 0);
  d.setUTCDate(d.getUTCDate() - (windowDays - 1));
  return d;
})();`, fmt.Sprintf("let rangeStart = new Date('%sT00:00:00Z');", from.Format("2006-01-02")), 1)
	js = strings.Replace(js, `let rangeEndDate = (() => { const d = new Date(); d.setUTCHours(0,0,0,0); return d; })();`, fmt.Sprintf("let rangeEndDate = new Date('%sT00:00:00Z');", to.Add(-time.Second).Format("2006-01-02")), 1)

	html = strings.Replace(html, `<script src="app.js"></script>`, "<script>\n"+js+"\n</script>", 1)

	script := fmt.Sprintf("<script>window.__DEMO__=%s;\n%s</script>", dataJSON, fetchShim)
	html = strings.Replace(html, "<head>", "<head>\n  "+script, 1)

	if err := os.MkdirAll(*outDir, 0755); err != nil {
		log.Fatalf("mkdir %s: %v", *outDir, err)
	}
	outPath := filepath.Join(*outDir, "index.html")
	if err := os.WriteFile(outPath, []byte(html), 0644); err != nil {
		log.Fatalf("write %s: %v", outPath, err)
	}
	log.Printf("wrote %s", outPath)
}

func parseDateRange(fromStr, toStr string) (from, to time.Time) {
	if fromStr == "" || toStr == "" {
		to = time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
		from = to.AddDate(0, 0, -30)
		return
	}
	var err error
	from, err = time.Parse("2006-01-02", fromStr)
	if err != nil {
		log.Fatalf("invalid --from: %v", err)
	}
	to, err = time.Parse("2006-01-02", toStr)
	if err != nil {
		log.Fatalf("invalid --to: %v", err)
	}
	to = to.Add(24 * time.Hour)
	return
}

func toTariffRates(rates []octopus.HalfHourlyRate) []tariff.Rate {
	result := make([]tariff.Rate, len(rates))
	for i, r := range rates {
		result[i] = tariff.Rate{
			ValueIncVAT: r.ValueIncVAT,
			ValidFrom:   r.ValidFrom,
			ValidTo:     r.ValidTo,
		}
	}
	return result
}

func toDeviceDays(days []solax.DayRecord) []device.DayData {
	result := make([]device.DayData, len(days))
	for i, d := range days {
		date, _ := time.Parse("2006-01-02", d.Date)
		result[i] = device.DayData{
			Date:             date,
			Resolution:       5 * time.Minute,
			TotalYield:       d.TotalYield,
			FeedIn:           d.FeedIn,
			GridImport:       d.GridImport,
			BatteryCharge:    d.BatteryCharge,
			BatteryDischarge: d.BatteryDischarge,
			Load:             d.TotalLoad,
			PVPower:          device.TimeSeries{Resolution: 5 * time.Minute, Values: d.PVPower},
			LoadPower:        device.TimeSeries{Resolution: 5 * time.Minute, Values: d.LoadPower},
			BatteryPower:     device.TimeSeries{Resolution: 5 * time.Minute, Values: d.BatteryPower},
			BatterySoC:       device.TimeSeries{Resolution: 5 * time.Minute, Values: d.BatterySoC},
		}
	}
	return result
}

func filterRatesForDay(rates []octopus.HalfHourlyRate, day time.Time) []float64 {
	result := make([]float64, 48)
	london, _ := time.LoadLocation("Europe/London")
	for _, r := range rates {
		local := r.ValidFrom.In(london)
		if local.Format("2006-01-02") != day.Format("2006-01-02") {
			continue
		}
		slot := local.Hour()*2 + local.Minute()/30
		if slot >= 0 && slot < 48 {
			result[slot] = r.ValueIncVAT
		}
	}
	return result
}

// fetchShim intercepts window.fetch and serves pre-baked data from
// window.__DEMO__ instead of hitting live API endpoints.
const fetchShim = `(function(){
  var D=window.__DEMO__;
  function filterByDate(arr,from,to){
    if(!arr||!from||!to)return arr;
    return arr.filter(function(x){var d=x.t?x.t.slice(0,10):x.date;return d>=from&&d<=to;});
  }
  var MAP={
    '/api/rates':function(p){return filterByDate(D[p.get('direction')==='export'?'rates-export':'rates-import'],p.get('from'),p.get('to'));},
    '/api/consumption':function(p){return filterByDate(D[p.get('direction')==='export'?'consumption-export':'consumption-import'],p.get('from'),p.get('to'));},
    '/api/analysis':function(p){var a=D['analysis']||{};return {days:filterByDate(a.days,p.get('from'),p.get('to')),import_periods:a.import_periods||[]};},
    '/api/battery/mode-switch':function(){return D['battery-mode-switch'];},
    '/api/battery/charging-optimisation':function(){return D['battery-charging'];},
    '/api/charging/optimization':function(p){var sc=D['smart-charging']||{};return {days:filterByDate(sc.days,p.get('from'),p.get('to')),summary:sc.summary||{}};}
  };
  var _f=window.fetch.bind(window);
  window.fetch=function(url){
    var rest=[].slice.call(arguments,1);
    var u=new URL(url,location.href);
    var fn=MAP[u.pathname];
    if(fn){var data=fn(u.searchParams);if(data!=null)return Promise.resolve(new Response(JSON.stringify(data),{status:200,headers:{'Content-Type':'application/json'}}));}
    return _f.apply(window,[url].concat(rest));
  };
})();`

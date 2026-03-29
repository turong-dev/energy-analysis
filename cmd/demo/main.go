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
	"energy-utility/internal/config"
	"energy-utility/internal/octopus"
	"energy-utility/internal/solax"
	"energy-utility/internal/store"
)

type dataPoint struct {
	T string  `json:"t"`
	V float64 `json:"v"`
}

type demoData struct {
	RatesImport       []dataPoint              `json:"rates-import"`
	RatesExport       []dataPoint              `json:"rates-export"`
	ConsumptionImport []dataPoint              `json:"consumption-import"`
	ConsumptionExport []dataPoint              `json:"consumption-export"`
	Analysis          analysis.AnalysisResult  `json:"analysis"`
	ModeSwitch        analysis.ModeSwitchResult `json:"battery-mode-switch"`
	Charging          analysis.ChargingOptResult `json:"battery-charging"`
}

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config file")
	fromStr := flag.String("from", "", "start date YYYY-MM-DD (default: 7 days ago)")
	toStr := flag.String("to", "", "end date YYYY-MM-DD inclusive (default: today)")
	outDir := flag.String("out", "demo", "output directory")
	uiSrc := flag.String("ui", "cmd/serve/ui/index.html", "path to source index.html")
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

	for _, dir := range []string{"import", "export"} {
		rates, err := octopus.ReadRates(ctx, s3, dir, from, to)
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

	importRates, err := octopus.ReadRates(ctx, s3, "import", from, to)
	if err != nil {
		log.Fatalf("analysis import rates: %v", err)
	}
	exportRates, err := octopus.ReadRates(ctx, s3, "export", from, to)
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

	d.Analysis = analysis.Calculate(importRates, exportRates, importCons, exportCons, importAgreements, exportAgreements, &cfg.Octopus)
	log.Printf("fetched analysis (%d days)", len(d.Analysis.Days))

	solaxDays, err := solax.ReadDays(ctx, s3)
	if err != nil {
		log.Fatalf("read solax days: %v", err)
	}
	d.ModeSwitch = analysis.DetectModeSwitch(solaxDays)
	d.Charging = analysis.AnalyseCharging(solaxDays)
	log.Printf("fetched battery data (%d solax days)", len(solaxDays))

	dataJSON, err := json.Marshal(d)
	if err != nil {
		log.Fatalf("marshal demo data: %v", err)
	}

	htmlBytes, err := os.ReadFile(*uiSrc)
	if err != nil {
		log.Fatalf("read %s: %v", *uiSrc, err)
	}

	script := fmt.Sprintf("<script>window.__DEMO__=%s;\n%s</script>", dataJSON, fetchShim)
	html := strings.Replace(string(htmlBytes), "<head>", "<head>\n  "+script, 1)

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
		from = to.AddDate(0, 0, -7)
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
    '/api/battery/charging-optimisation':function(){return D['battery-charging'];}
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

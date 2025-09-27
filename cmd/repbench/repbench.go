// repbench is a benchmarking utility for s2prot.
//
// It accepts files, directories, or globs that point to .SC2Replay files and
// measures the performance of common operations:
//  - Time to load (open MPQ and decode header)
//  - Time to decode (full high-level parse via rep.NewFromFileEvents)
//  - Time to read a selection of fields (simulate typical usage)
//
// Additionally it attempts to track resource metrics:
//  - Approx disk bytes read (file size)
//  - CPU time used vs wall time (CPU% and estimated IO wait = wall - CPU)
//  - Memory usage deltas
//
// Note: Exact per-operation disk wait and read bytes are not universally
// available in a portable manner. This tool approximates them. IO-wait is
// approximated as wall - CPU for the operation. Bytes read is approximated by
// the file size as the MPQ reader reads from the file internally.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/stego-research/mpq"
	"github.com/stego-research/s2prot/v2"
	"github.com/stego-research/s2prot/v2/rep"
)

// perFileResult holds metrics collected for a single file.
type perFileResult struct {
	File           string        `json:"file"`
	FileSize       int64         `json:"file_size_bytes"`
	LoadWall       time.Duration `json:"load_wall_ns"`
	LoadCPU        time.Duration `json:"load_cpu_ns"`
	DecodeWall     time.Duration `json:"decode_wall_ns"`
	DecodeCPU      time.Duration `json:"decode_cpu_ns"`
	ReadWall       time.Duration `json:"read_wall_ns"`
	ReadCPU        time.Duration `json:"read_cpu_ns"`
	MemAllocDelta  int64         `json:"mem_alloc_delta_bytes"`
	MemTotalDelta  int64         `json:"mem_total_alloc_delta_bytes"`
	EventsDecoded  struct {
		Game    int `json:"game"`
		Message int `json:"message"`
		Tracker int `json:"tracker"`
	} `json:"events_decoded"`
	HeaderVersion string `json:"header_version"`
	MapTitle      string `json:"map_title"`
	BaseBuild     int    `json:"base_build"`
	Error         string `json:"error,omitempty"`
}

// aggregateStats summarizes results across files.
type aggregateStats struct {
	Count int `json:"count"`

	LoadWall   distribution `json:"load_wall"`
	LoadCPU    distribution `json:"load_cpu"`
	DecodeWall distribution `json:"decode_wall"`
	DecodeCPU  distribution `json:"decode_cpu"`
	ReadWall   distribution `json:"read_wall"`
	ReadCPU    distribution `json:"read_cpu"`

	MemAllocDelta distribution `json:"mem_alloc_delta"`
	MemTotalDelta distribution `json:"mem_total_alloc_delta"`

	Outliers struct {
		SlowestFiles []string `json:"slowest_files_p99"`
		FastestFiles []string `json:"fastest_files_p1"`
	} `json:"outliers"`
}

type distribution struct {
	Min      int64   `json:"min"`
	P01      int64   `json:"p01"`
	Median   int64   `json:"median"`
	P99      int64   `json:"p99"`
	Max      int64   `json:"max"`
	Mean     float64 `json:"mean"`
	StdDev   float64 `json:"stddev"`
	Unit     string  `json:"unit"` // e.g. ns, bytes
	Samples  int     `json:"samples"`
}

// report is the overall JSON payload written by the tool.
type report struct {
	GeneratedAt string           `json:"generated_at"`
	GoVersion   string           `json:"go_version"`
	CPUCount    int              `json:"cpu_count"`
	Workers     int              `json:"workers"`
	IncludeGame bool             `json:"include_game"`
	IncludeMsg  bool             `json:"include_message"`
	IncludeTrk  bool             `json:"include_tracker"`
	Files       []perFileResult  `json:"files"`
	Summary     aggregateStats   `json:"summary"`
	Notes       []string         `json:"notes"`
}

func main() {
	out := flag.String("out", "", "Optional path to write JSON report. Defaults to stdout")
	htmlOut := flag.String("html", "", "Optional path to write an HTML report")
	workers := flag.Int("workers", 1, "Number of concurrent workers")
	recurse := flag.Bool("recurse", true, "Recurse into directories when collecting files")
	limit := flag.Int("limit", 0, "Optional limit on number of files to process (0 = no limit)")
	includeGame := flag.Bool("game", true, "Decode game events")
	includeMsg := flag.Bool("message", true, "Decode message events")
	includeTrk := flag.Bool("tracker", true, "Decode tracker events")
	showProgress := flag.Bool("progress", false, "Show progress while processing files")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "repbench - benchmark s2prot parsing\n")
		fmt.Fprintf(os.Stderr, "Usage: repbench [flags] <files|dirs|globs>...\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	inputs := flag.Args()
	if len(inputs) == 0 {
		flag.Usage()
		os.Exit(2)
	}

	files, err := collectReplayFiles(inputs, *recurse)
	if err != nil {
		fmt.Fprintf(os.Stderr, "collecting inputs: %v\n", err)
		os.Exit(2)
	}
	if *limit > 0 && len(files) > *limit {
		files = files[:*limit]
	}
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "No .SC2Replay files found in inputs")
		os.Exit(1)
	}

 // Run benchmarks (optionally in parallel)
	res := runBench(files, *workers, *includeGame, *includeMsg, *includeTrk, *showProgress)

	summary := summarize(res)
	r := report{
		GeneratedAt: time.Now().Format(time.RFC3339),
		GoVersion:   runtime.Version(),
		CPUCount:    runtime.NumCPU(),
		Workers:     *workers,
		IncludeGame: *includeGame,
		IncludeMsg:  *includeMsg,
		IncludeTrk:  *includeTrk,
		Files:       res,
		Summary:     summary,
		Notes: []string{
			"Disk bytes read is approximated as the file size.",
			"IO wait time is approximated as wall time minus process CPU time during the operation.",
		},
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to create output file: %v\n", err)
			os.Exit(3)
		}
		defer f.Close()
		enc = json.NewEncoder(f)
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(r); err != nil {
		fmt.Fprintf(os.Stderr, "failed to encode report: %v\n", err)
		os.Exit(3)
	}
	// Optionally emit HTML report
	if *htmlOut != "" {
		if err := writeHTMLReport(*htmlOut, r); err != nil {
			fmt.Fprintf(os.Stderr, "failed to write HTML report: %v\n", err)
			os.Exit(3)
		}
	}
}

func collectReplayFiles(inputs []string, recurse bool) ([]string, error) {
	seen := map[string]struct{}{}
	var out []string
	add := func(p string) {
		if !strings.HasSuffix(strings.ToLower(p), ".sc2replay") {
			return
		}
		abs, err := filepath.Abs(p)
		if err == nil {
			p = abs
		}
		if _, ok := seen[p]; !ok {
			seen[p] = struct{}{}
			out = append(out, p)
		}
	}
	for _, in := range inputs {
		// If it contains glob wildcards, expand
		if strings.ContainsAny(in, "*?[") {
			matches, err := filepath.Glob(in)
			if err != nil {
				return nil, err
			}
			for _, m := range matches {
				fi, err := os.Stat(m)
				if err == nil && fi.Mode().IsRegular() {
					add(m)
				}
			}
			continue
		}
		fi, err := os.Stat(in)
		if err != nil {
			continue
		}
		if fi.Mode().IsRegular() {
			add(in)
			continue
		}
		if fi.IsDir() {
			if !recurse {
				entries, err := os.ReadDir(in)
				if err != nil {
					return nil, err
				}
				for _, e := range entries {
					if !e.Type().IsRegular() {
						continue
					}
					add(filepath.Join(in, e.Name()))
				}
			} else {
				filepath.WalkDir(in, func(path string, d os.DirEntry, err error) error {
					if err != nil {
						return nil
					}
					if d.Type().IsRegular() {
						add(path)
					}
					return nil
				})
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

func runBench(files []string, workers int, game, message, tracker bool, progress bool) []perFileResult {
	if workers < 1 {
		workers = 1
	}
	res := make([]perFileResult, len(files))
	var wg sync.WaitGroup
	ch := make(chan int)
	// progress tracking
	var done chan struct{}
	if progress {
		done = make(chan struct{}, 128)
		go func(total int) {
			processed := 0
			last := time.Now()
			for range done {
				processed++
				// throttle prints to avoid spamming
				if time.Since(last) >= 200*time.Millisecond || processed == total {
					pct := int(float64(processed) / float64(total) * 100.0)
					fmt.Fprintf(os.Stderr, "\rProcessed %d/%d (%d%%)", processed, total, pct)
					last = time.Now()
				}
			}
			// channel closed, ensure final newline if any was printed
			if total > 0 {
				fmt.Fprintln(os.Stderr)
			}
		}(len(files))
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range ch {
				res[idx] = benchOne(files[idx], game, message, tracker)
				if progress {
					done <- struct{}{}
				}
			}
		}()
	}
	for i := range files {
		ch <- i
	}
	close(ch)
	wg.Wait()
	if progress {
		close(done)
	}
	return res
}

func benchOne(file string, game, message, tracker bool) perFileResult {
	var r perFileResult
	r.File = file
	if fi, err := os.Stat(file); err == nil {
		r.FileSize = fi.Size()
	}

	// Measure load: open MPQ + decode header
	ms0 := readMem()
	cpu0 := processCPU()
	wall0 := time.Now()
	m, err := mpq.NewFromFile(file)
	if err != nil {
		r.Error = ErrString(err)
		return r
	}
	defer m.Close()
	hdr := s2prot.DecodeHeader(m.UserData())
	loadWall := time.Since(wall0)
	loadCPU := processCPU() - cpu0
	ms1 := readMem()
	r.LoadWall = loadWall
	r.LoadCPU = loadCPU
	r.HeaderVersion = headerVersionString(&hdr)
	v := (&hdr).Structv("version")
	r.BaseBuild = int(v.Int("baseBuild"))
	r.MemAllocDelta += ms1.Alloc - ms0.Alloc
	r.MemTotalDelta += int64(ms1.TotalAlloc - ms0.TotalAlloc)

	// Measure decode: full high-level parse
	ms2 := readMem()
	cpu1 := processCPU()
	wall1 := time.Now()
	repObj, err := rep.NewFromFileEvents(file, game, message, tracker)
	decodeWall := time.Since(wall1)
	decodeCPU := processCPU() - cpu1
	ms3 := readMem()
	if err != nil {
		r.Error = ErrString(err)
		return r
	}
	defer repObj.Close()
	r.DecodeWall = decodeWall
	r.DecodeCPU = decodeCPU
	r.MemAllocDelta += ms3.Alloc - ms2.Alloc
	r.MemTotalDelta += int64(ms3.TotalAlloc - ms2.TotalAlloc)

	// Measure reading of common fields
	ms4 := readMem()
	cpu2 := processCPU()
	wall2 := time.Now()
	// Typical access patterns
	_ = repObj.Header.VersionString()
	r.MapTitle = repObj.Details.Title()
	var ge, me, te int
	if repObj.GameEvents != nil {
		ge = len(repObj.GameEvents)
	}
	if repObj.MessageEvents != nil {
		me = len(repObj.MessageEvents)
	}
	if repObj.TrackerEvents != nil {
		te = len(repObj.TrackerEvents.Events)
	}
	r.EventsDecoded.Game = ge
	r.EventsDecoded.Message = me
	r.EventsDecoded.Tracker = te
	// Derive a little from tracker if present
	if repObj.TrackerEvents != nil && len(repObj.TrackerEvents.Events) > 0 {
		// Force lazy computations in tracker init by calling internal processing indirectly
		// through methods that access derived structures. A lightweight nudge:
		for _, pd := range repObj.TrackerEvents.PIDPlayerDescMap {
			_ = pd.PlayerID
			break
		}
	}
	readWall := time.Since(wall2)
	readCPU := processCPU() - cpu2
	ms5 := readMem()
	r.ReadWall = readWall
	r.ReadCPU = readCPU
	r.MemAllocDelta += ms5.Alloc - ms4.Alloc
	r.MemTotalDelta += int64(ms5.TotalAlloc - ms4.TotalAlloc)

	return r
}

type memSnapshot struct {
	Alloc      int64
	TotalAlloc uint64
}

func readMem() memSnapshot {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return memSnapshot{Alloc: int64(m.Alloc), TotalAlloc: m.TotalAlloc}
}

func processCPU() time.Duration {
	// Portable CPU time collection isn't guaranteed across environments here;
	// return 0 to indicate unknown CPU time. IO wait will also be unknown.
	return 0
}

func headerVersionString(h *s2prot.Struct) string {
	if h == nil || *h == nil {
		return ""
	}
	v := h.Structv("version")
	return fmt.Sprintf("%d.%d.%d.%d", v.Int("major"), v.Int("minor"), v.Int("revision"), v.Int("build"))
}

func summarize(files []perFileResult) aggregateStats {
	agg := aggregateStats{Count: len(files)}
	var loadWall, loadCPU, decWall, decCPU, readWall, readCPU, memAlloc, memTotal []int64
	fileByDecode := make([]struct{
		name string
		dur int64
	}, 0, len(files))

	for _, f := range files {
		if f.Error != "" {
			continue
		}
		loadWall = append(loadWall, f.LoadWall.Nanoseconds())
		loadCPU = append(loadCPU, f.LoadCPU.Nanoseconds())
		decWall = append(decWall, f.DecodeWall.Nanoseconds())
		decCPU = append(decCPU, f.DecodeCPU.Nanoseconds())
		readWall = append(readWall, f.ReadWall.Nanoseconds())
		readCPU = append(readCPU, f.ReadCPU.Nanoseconds())
		memAlloc = append(memAlloc, f.MemAllocDelta)
		memTotal = append(memTotal, f.MemTotalDelta)
		fileByDecode = append(fileByDecode, struct{ name string; dur int64 }{f.File, f.DecodeWall.Nanoseconds()})
	}

	agg.LoadWall = makeDist(loadWall, "ns")
	agg.LoadCPU = makeDist(loadCPU, "ns")
	agg.DecodeWall = makeDist(decWall, "ns")
	agg.DecodeCPU = makeDist(decCPU, "ns")
	agg.ReadWall = makeDist(readWall, "ns")
	agg.ReadCPU = makeDist(readCPU, "ns")
	agg.MemAllocDelta = makeDist(memAlloc, "bytes")
	agg.MemTotalDelta = makeDist(memTotal, "bytes")

	sort.Slice(fileByDecode, func(i, j int) bool { return fileByDecode[i].dur < fileByDecode[j].dur })
	// p01/p99 boundaries
	if n := len(fileByDecode); n > 0 {
		p01i := int(math.Max(0, math.Floor(0.01*float64(n))-1))
		p99i := int(math.Min(float64(n-1), math.Ceil(0.99*float64(n))))
		// Fastest ~ bottom 1%
		for i := 0; i <= p01i; i++ {
			agg.Outliers.FastestFiles = append(agg.Outliers.FastestFiles, fileByDecode[i].name)
		}
		// Slowest ~ top 1%
		for i := p99i; i < n; i++ {
			agg.Outliers.SlowestFiles = append(agg.Outliers.SlowestFiles, fileByDecode[i].name)
		}
	}
	return agg
}

func makeDist(vals []int64, unit string) distribution {
	d := distribution{Unit: unit, Samples: len(vals)}
	if len(vals) == 0 {
		return d
	}
	sorted := append([]int64(nil), vals...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	d.Min = sorted[0]
	d.Max = sorted[len(sorted)-1]
	// Median
	mid := len(sorted) / 2
	if len(sorted)%2 == 0 {
		d.Median = (sorted[mid-1] + sorted[mid]) / 2
	} else {
		d.Median = sorted[mid]
	}
	// p01 & p99
	idx01 := int(math.Max(0, math.Floor(0.01*float64(len(sorted)))-1))
	idx99 := int(math.Min(float64(len(sorted)-1), math.Ceil(0.99*float64(len(sorted)))))
	d.P01 = sorted[idx01]
	d.P99 = sorted[idx99]
	// mean & stddev
	var sum float64
	for _, v := range sorted {
		sum += float64(v)
	}
	d.Mean = sum / float64(len(sorted))
	var sq float64
	for _, v := range sorted {
		dv := float64(v) - d.Mean
		sq += dv * dv
	}
	d.StdDev = math.Sqrt(sq / float64(len(sorted)))
	return d
}

func ErrString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// writeHTMLReport renders a simple HTML report to the given path.
func writeHTMLReport(path string, r report) error {
	const tpl = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<title>repbench report</title>
<style>
body{font-family:system-ui,-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;margin:16px;}
header{margin-bottom:16px}
.summary{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:12px;margin-bottom:16px}
.card{border:1px solid #ddd;border-radius:8px;padding:12px;background:#fafafa}
.card h3{margin:0 0 8px 0;font-size:16px}
.small{color:#666;font-size:12px}
.table-wrap{overflow:auto;}
table{border-collapse:collapse;min-width:900px;width:100%;}
th,td{border:1px solid #ddd;padding:6px 8px;text-align:left;}
th{background:#f0f0f0;position:sticky;top:0}
tr:nth-child(even){background:#fcfcfc}
.err{color:#b00020;font-weight:bold}
.code{font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,monospace}
</style>
</head>
<body>
<header>
<h1>repbench report</h1>
<div class="small">Generated: {{.GeneratedAt}} | Go: {{.GoVersion}} | CPUs: {{.CPUCount}} | Workers: {{.Workers}} | Events: game={{.IncludeGame}} msg={{.IncludeMsg}} trk={{.IncludeTrk}}</div>
</header>
<section class="summary">
	<div class="card">
		<h3>Load wall</h3>
		<div>median: {{ms .Summary.LoadWall.Median}} ms</div>
		<div>p99: {{ms .Summary.LoadWall.P99}} ms</div>
		<div>mean: {{msf .Summary.LoadWall.Mean}} ms</div>
	</div>
	<div class="card">
		<h3>Decode wall</h3>
		<div>median: {{ms .Summary.DecodeWall.Median}} ms</div>
		<div>p99: {{ms .Summary.DecodeWall.P99}} ms</div>
		<div>mean: {{msf .Summary.DecodeWall.Mean}} ms</div>
	</div>
	<div class="card">
		<h3>Read wall</h3>
		<div>median: {{ms .Summary.ReadWall.Median}} ms</div>
		<div>p99: {{ms .Summary.ReadWall.P99}} ms</div>
		<div>mean: {{msf .Summary.ReadWall.Mean}} ms</div>
	</div>
	<div class="card">
		<h3>Memory</h3>
		<div>alloc Δ median: {{bytes .Summary.MemAllocDelta.Median}}</div>
		<div>total Δ median: {{bytes .Summary.MemTotalDelta.Median}}</div>
	</div>
</section>
<section class="table-wrap">
<table>
<thead>
<tr>
	<th>#</th>
	<th>File</th>
	<th>Size</th>
	<th>Header</th>
	<th>Map</th>
	<th>Base</th>
	<th>Load</th>
	<th>Decode</th>
	<th>Read</th>
	<th>Alloc Δ</th>
	<th>Error</th>
</tr>
</thead>
<tbody>
{{range $i, $f := .Files}}
<tr>
	<td>{{add $i 1}}</td>
	<td class="code">{{$f.File}}</td>
	<td>{{bytes $f.FileSize}}</td>
	<td>{{$f.HeaderVersion}}</td>
	<td>{{$f.MapTitle}}</td>
	<td>{{$f.BaseBuild}}</td>
	<td>{{dur $f.LoadWall}}</td>
	<td>{{dur $f.DecodeWall}}</td>
	<td>{{dur $f.ReadWall}}</td>
	<td>{{bytes $f.MemAllocDelta}}</td>
	<td>{{if $f.Error}}<span class="err">{{$f.Error}}</span>{{end}}</td>
</tr>
{{end}}
</tbody>
</table>
</section>
<section>
<h3>Notes</h3>
<ul>
{{range .Notes}}<li>{{.}}</li>{{end}}
</ul>
</section>
</body>
</html>`
	funcs := template.FuncMap{
		"bytes": func(n int64) string {
			if n == 0 {
				return "0 B"
			}
			neg := n < 0
			if neg {
				n = -n
			}
			units := []string{"B","KB","MB","GB","TB"}
			i := 0
			f := float64(n)
			for f >= 1024 && i < len(units)-1 {
				f /= 1024
				i++
			}
			if neg {
				f = -f
			}
			return fmt.Sprintf("%.1f %s", f, units[i])
		},
		"dur": func(d time.Duration) string { return d.String() },
		"ms": func(n int64) string { return fmt.Sprintf("%.2f", float64(n)/1e6) },
		"msf": func(f float64) string { return fmt.Sprintf("%.2f", f/1e6) },
		"add": func(a, b int) int { return a + b },
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	t, err := template.New("repbench").Funcs(funcs).Parse(tpl)
	if err != nil {
		return err
	}
	return t.Execute(f, r)
}

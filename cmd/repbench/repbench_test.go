package main

import (
    "errors"
    "os"
    "path/filepath"
    "runtime"
    "strings"
    "testing"
    "time"

    "github.com/stego-research/s2prot/v2"
)

func TestCollectReplayFiles(t *testing.T) {
    dir := t.TempDir()
    // Files
    f1 := filepath.Join(dir, "a.SC2Replay")
    f2 := filepath.Join(dir, "b.sc2replay") // lowercase extension
    f3 := filepath.Join(dir, "c.txt")       // should be ignored
    if err := os.WriteFile(f1, []byte("x"), 0o644); err != nil { t.Fatal(err) }
    if err := os.WriteFile(f2, []byte("y"), 0o644); err != nil { t.Fatal(err) }
    if err := os.WriteFile(f3, []byte("z"), 0o644); err != nil { t.Fatal(err) }

    // Subdir with files
    sub := filepath.Join(dir, "sub")
    if err := os.Mkdir(sub, 0o755); err != nil { t.Fatal(err) }
    f4 := filepath.Join(sub, "d.SC2Replay")
    f5 := filepath.Join(sub, "e.txt")
    if err := os.WriteFile(f4, []byte("w"), 0o644); err != nil { t.Fatal(err) }
    if err := os.WriteFile(f5, []byte("w"), 0o644); err != nil { t.Fatal(err) }

    // 1) Direct file inputs
    got, err := collectReplayFiles([]string{f1, f2, f3}, false)
    if err != nil { t.Fatalf("collect err: %v", err) }
    if len(got) != 2 { t.Fatalf("want 2 files, got %d: %v", len(got), got) }

    // 2) Directory without recurse (should include only top-level SC2Replay)
    got, err = collectReplayFiles([]string{dir}, false)
    if err != nil { t.Fatalf("collect err: %v", err) }
    if len(got) != 2 { t.Fatalf("want 2 files from top-level, got %d: %v", len(got), got) }

    // 3) Directory with recurse (should include subdir replay too)
    got, err = collectReplayFiles([]string{dir}, true)
    if err != nil { t.Fatalf("collect err: %v", err) }
    if len(got) != 3 { t.Fatalf("want 3 files including subdir, got %d: %v", len(got), got) }

    // 4) Glob
    pattern := filepath.Join(dir, "*.SC2Replay")
    got, err = collectReplayFiles([]string{pattern}, false)
    if err != nil { t.Fatalf("collect err: %v", err) }
    if len(got) != 1 || !strings.HasSuffix(strings.ToLower(got[0]), "a.sc2replay") {
        t.Fatalf("glob mismatch: %v", got)
    }

    // 5) Dedup (the function dedups logically). Give the same file twice.
    got, err = collectReplayFiles([]string{f1, f1}, false)
    if err != nil { t.Fatalf("collect err: %v", err) }
    if len(got) != 1 { t.Fatalf("expected dedup to 1, got %d", len(got)) }
}

func TestMakeDist(t *testing.T) {
    // Deterministic set: 1..10
    var vals []int64
    for i := int64(1); i <= 10; i++ { vals = append(vals, i) }
    d := makeDist(vals, "units")
    if d.Min != 1 || d.Max != 10 { t.Fatalf("min/max got (%d,%d)", d.Min, d.Max) }
    if d.Median != (5+6)/2 { t.Fatalf("median got %d", d.Median) }
    if d.P01 < d.Min || d.P99 > d.Max { t.Fatalf("p01/p99 out of range: %d/%d", d.P01, d.P99) }
    // Mean of 1..10 is 5.5
    if d.Mean < 5.49 || d.Mean > 5.51 { t.Fatalf("mean got %f", d.Mean) }
    // Stddev population of 1..10 is sqrt(8.25) ~= 2.872281323
    if d.StdDev < 2.87 || d.StdDev > 2.88 { t.Fatalf("stddev got %f", d.StdDev) }
    if d.Unit != "units" || d.Samples != 10 { t.Fatalf("meta mismatch: %+v", d) }

    // Empty input
    d = makeDist(nil, "ns")
    if d.Samples != 0 || d.Unit != "ns" { t.Fatalf("empty dist mismatch: %+v", d) }
}

func TestErrString(t *testing.T) {
    if s := ErrString(nil); s != "" { t.Fatalf("want empty, got %q", s) }
    e := errors.New("boom")
    if s := ErrString(e); s != "boom" { t.Fatalf("want 'boom', got %q", s) }
}

func TestHeaderVersionString(t *testing.T) {
    // Nil or empty struct
    var s s2prot.Struct
    if got := headerVersionString(&s); got != "" { t.Fatalf("want empty for nil struct, got %q", got) }

    // Construct a minimal struct with version fields
    s = s2prot.Struct{
        "version": s2prot.Struct{
            "major": int64(3),
            "minor": int64(2),
            "revision": int64(1),
            "build": int64(45678),
        },
    }
    got := headerVersionString(&s)
    if got != "3.2.1.45678" { t.Fatalf("unexpected version string: %q", got) }
}

func TestWriteHTMLReport(t *testing.T) {
    tmp := t.TempDir()
    out := filepath.Join(tmp, "report.html")
    r := report{
        GeneratedAt: time.Now().Format(time.RFC3339),
        GoVersion: runtime.Version(),
        CPUCount: runtime.NumCPU(),
        Workers: 1,
        Files: []perFileResult{{
            File: "x.SC2Replay",
            FileSize: 123,
            HeaderVersion: "1.2.3.4",
            MapTitle: "Some Map",
            BaseBuild: 55555,
        }},
        Summary: aggregateStats{},
        Notes: []string{"note1"},
    }
    if err := writeHTMLReport(out, r); err != nil { t.Fatalf("writeHTMLReport: %v", err) }
    b, err := os.ReadFile(out)
    if err != nil { t.Fatalf("read html: %v", err) }
    html := string(b)
    // Spot-check a couple key strings made it into the report
    for _, want := range []string{"repbench report", "Some Map", "x.SC2Replay"} {
        if !strings.Contains(html, want) {
            t.Fatalf("html does not contain %q; got: %.120s...", want, html)
        }
    }
}

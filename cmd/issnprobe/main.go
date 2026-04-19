// issnprobe verifies registration status of candidate ISSN against the
// issn.org per-resource content-negotiation endpoint, with aggressive
// caching and conservative rate limiting.
//
// The portal is paywalled and its sitemap has been removed; the only
// remaining open endpoint is:
//
//	curl -H "Accept: application/ld+json" \
//	     https://portal.issn.org/resource/ISSN/<NNNN-NNNC>
//
// We use this endpoint, serially, with a minimum inter-request delay,
// exponential backoff on 429/5xx, and a per-ISSN disk cache. See
// notes/2026-04-19-approximation.md for the estimation framework.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/adrg/xdg"
)

const (
	defaultUA   = "issnprobe/0.1.0 (+https://github.com/miku/issnlister)"
	jsonLDType  = "application/ld+json"
	lookupURLFmt = "https://portal.issn.org/resource/ISSN/%s"
	version     = "0.1.0"
)

// ----- ISSN math ----------------------------------------------------------

// issnCheckDigit computes the check digit for a 7-digit prefix. Returns
// the empty string if the input is not 7 ASCII digits.
func issnCheckDigit(seven string) string {
	if len(seven) != 7 {
		return ""
	}
	sum := 0
	for i := 0; i < 7; i++ {
		c := seven[i]
		if c < '0' || c > '9' {
			return ""
		}
		sum += int(c-'0') * (8 - i)
	}
	mod := sum % 11
	cd := 0
	if mod != 0 {
		cd = 11 - mod
	}
	if cd == 10 {
		return "X"
	}
	return strconv.Itoa(cd)
}

// formatISSN turns a 7-digit prefix into a hyphenated ISSN with check
// digit, e.g. "3134164" -> "3134-1640".
func formatISSN(seven string) string {
	cd := issnCheckDigit(seven)
	if cd == "" {
		return ""
	}
	return seven[:4] + "-" + seven[4:] + cd
}

// blockCandidates returns the 1000 checksum-valid ISSN inside a 4-digit
// prefix, in ascending numeric order.
func blockCandidates(prefix4 string) []string {
	out := make([]string, 0, 1000)
	for i := 0; i < 1000; i++ {
		seven := fmt.Sprintf("%s%03d", prefix4, i)
		if s := formatISSN(seven); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// ----- known set ----------------------------------------------------------

func loadKnown(path string) (map[string]struct{}, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	known := make(map[string]struct{}, 3_000_000)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" {
			known[line] = struct{}{}
		}
	}
	return known, sc.Err()
}

// ----- cache + HTTP -------------------------------------------------------

// Result is the per-ISSN cache record.
type Result struct {
	ISSN       string    `json:"issn"`
	Status     int       `json:"status"`
	Registered bool      `json:"registered"`
	FetchedAt  time.Time `json:"fetched_at"`
	Error      string    `json:"error,omitempty"`
}

type Prober struct {
	client   *http.Client
	cacheDir string
	ua       string
	delay    time.Duration
	saveBody bool

	// backoff state
	minBackoff time.Duration
	maxBackoff time.Duration
	maxRetries int
}

func (p *Prober) cachePath(issn string) string {
	return filepath.Join(p.cacheDir, issn[:4], issn+".json")
}

func (p *Prober) bodyPath(issn string) string {
	return filepath.Join(p.cacheDir, issn[:4], issn+".jsonld")
}

func (p *Prober) readCache(issn string) (*Result, bool) {
	b, err := os.ReadFile(p.cachePath(issn))
	if err != nil {
		return nil, false
	}
	var r Result
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, false
	}
	return &r, true
}

func (p *Prober) writeCache(r *Result, body []byte) error {
	dir := filepath.Dir(p.cachePath(r.ISSN))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if err := os.WriteFile(p.cachePath(r.ISSN), b, 0o644); err != nil {
		return err
	}
	if p.saveBody && r.Registered && len(body) > 0 {
		_ = os.WriteFile(p.bodyPath(r.ISSN), body, 0o644)
	}
	return nil
}

// probe fetches one ISSN with retries. Cache hits are returned without
// network access. Network errors and 429/5xx trigger exponential
// backoff (honouring Retry-After when present). 404 is a terminal,
// valid "not registered" response.
func (p *Prober) probe(ctx context.Context, issn string) (*Result, error) {
	if r, ok := p.readCache(issn); ok {
		return r, nil
	}
	url := fmt.Sprintf(lookupURLFmt, issn)
	backoff := p.minBackoff
	var (
		status int
		body   []byte
		lastErr error
	)
	for attempt := 0; attempt <= p.maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", jsonLDType)
		req.Header.Set("User-Agent", p.ua)
		resp, err := p.client.Do(req)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if err := sleepCtx(ctx, backoff); err != nil {
				return nil, err
			}
			backoff = nextBackoff(backoff, p.maxBackoff)
			continue
		}
		status = resp.StatusCode
		if status == http.StatusTooManyRequests || status >= 500 {
			wait := backoff
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if s, err := strconv.Atoi(ra); err == nil && s > 0 {
					wait = time.Duration(s) * time.Second
				}
			}
			resp.Body.Close()
			if err := sleepCtx(ctx, wait); err != nil {
				return nil, err
			}
			backoff = nextBackoff(backoff, p.maxBackoff)
			lastErr = fmt.Errorf("http %d", status)
			continue
		}
		body, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		lastErr = nil
		break
	}
	r := &Result{
		ISSN:      issn,
		Status:    status,
		FetchedAt: time.Now().UTC(),
	}
	// A registered ISSN returns 200 with a JSON-LD document that
	// contains an @context token. Bare 200 with empty/HTML body is
	// not treated as registered.
	if status == http.StatusOK && len(body) > 0 && strings.Contains(string(body), `"@context"`) {
		r.Registered = true
	}
	if lastErr != nil && status == 0 {
		r.Error = lastErr.Error()
	}
	_ = p.writeCache(r, body)
	return r, nil
}

func nextBackoff(cur, max time.Duration) time.Duration {
	n := cur * 2
	if n > max {
		return max
	}
	return n
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// ----- candidate generation ----------------------------------------------

type candidateSpec struct {
	mode       string
	prefixMin  int
	prefixMax  int
	sparseMax  int
	sampleN    int
	seed       int64
	stopAfter  int // frontier: stop per block after this many consecutive misses (0 = no stop)
}

func buildCandidates(known map[string]struct{}, density map[string]int, spec candidateSpec) ([]string, error) {
	switch spec.mode {
	case "estimate":
		return estimatePool(known, spec), nil
	case "sparse":
		return sparseCandidates(known, density, spec), nil
	case "frontier":
		return frontierCandidates(known, density, spec), nil
	default:
		return nil, fmt.Errorf("unknown mode: %s", spec.mode)
	}
}

func estimatePool(known map[string]struct{}, spec candidateSpec) []string {
	rng := rand.New(rand.NewSource(spec.seed))
	pool := make([]string, 0, 1_000_000)
	for p := spec.prefixMin; p <= spec.prefixMax; p++ {
		prefix4 := fmt.Sprintf("%04d", p)
		for _, c := range blockCandidates(prefix4) {
			if _, ok := known[c]; !ok {
				pool = append(pool, c)
			}
		}
	}
	log.Printf("unknown pool over [%04d..%04d]: %d", spec.prefixMin, spec.prefixMax, len(pool))
	rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	n := spec.sampleN
	if n > len(pool) {
		n = len(pool)
	}
	return pool[:n]
}

func sparseCandidates(known map[string]struct{}, density map[string]int, spec candidateSpec) []string {
	out := make([]string, 0, 50_000)
	for p := spec.prefixMin; p <= spec.prefixMax; p++ {
		prefix4 := fmt.Sprintf("%04d", p)
		if density[prefix4] > spec.sparseMax {
			continue
		}
		for _, c := range blockCandidates(prefix4) {
			if _, ok := known[c]; !ok {
				out = append(out, c)
			}
		}
	}
	return out
}

func frontierCandidates(known map[string]struct{}, density map[string]int, spec candidateSpec) []string {
	out := make([]string, 0, 20_000)
	for p := spec.prefixMin; p <= spec.prefixMax; p++ {
		prefix4 := fmt.Sprintf("%04d", p)
		d := density[prefix4]
		if d == 0 {
			// allocate the first few candidates of a fresh block
			cc := blockCandidates(prefix4)
			if len(cc) > 32 {
				cc = cc[:32]
			}
			out = append(out, cc...)
			continue
		}
		if d >= 990 {
			continue
		}
		cc := blockCandidates(prefix4)
		// next-after-max in known; then everything after.
		maxIdx := -1
		for i, c := range cc {
			if _, ok := known[c]; ok {
				maxIdx = i
			}
		}
		for i := maxIdx + 1; i < len(cc); i++ {
			out = append(out, cc[i])
		}
	}
	return out
}

// ----- stats --------------------------------------------------------------

type estimate struct {
	Probes     int     `json:"probes"`
	Hits       int     `json:"hits"`
	PoolSize   int     `json:"pool_size"`
	PHat       float64 `json:"p_hat"`
	NHat       float64 `json:"n_hat"`
	NormalLo   float64 `json:"normal_ci_lo"`
	NormalHi   float64 `json:"normal_ci_hi"`
	WilsonLo   float64 `json:"wilson_ci_lo"`
	WilsonHi   float64 `json:"wilson_ci_hi"`
	MarginAbs  float64 `json:"normal_margin_abs"` // half-width on N̂
}

func computeEstimate(hits, probes, pool int) estimate {
	e := estimate{Probes: probes, Hits: hits, PoolSize: pool}
	if probes == 0 {
		return e
	}
	z := 1.96
	p := float64(hits) / float64(probes)
	e.PHat = p
	e.NHat = p * float64(pool)
	se := math.Sqrt(p * (1 - p) / float64(probes))
	e.NormalLo = math.Max(0, p-z*se) * float64(pool)
	e.NormalHi = math.Min(1, p+z*se) * float64(pool)
	e.MarginAbs = z * se * float64(pool)
	// Wilson
	n := float64(probes)
	denom := 1 + z*z/n
	centre := (p + z*z/(2*n)) / denom
	half := z * math.Sqrt(p*(1-p)/n+z*z/(4*n*n)) / denom
	e.WilsonLo = math.Max(0, centre-half) * float64(pool)
	e.WilsonHi = math.Min(1, centre+half) * float64(pool)
	return e
}

// ----- main ---------------------------------------------------------------

func main() {
	var (
		issnPath    = flag.String("f", "issn.tsv", "path to known ISSN list (one per line)")
		cacheDir    = flag.String("d", "", "cache dir (default XDG_CACHE_HOME/issnprobe)")
		mode        = flag.String("mode", "estimate", "candidate mode: estimate | sparse | frontier")
		delayMs     = flag.Int("delay", 3000, "minimum ms between network requests")
		saveBody    = flag.Bool("save-body", true, "save JSON-LD body for registered ISSN")
		prefixMin   = flag.String("prefix-min", "0000", "4-digit min prefix, inclusive")
		prefixMax   = flag.String("prefix-max", "3199", "4-digit max prefix, inclusive")
		sampleN     = flag.Int("n", 400, "sample size (estimate mode)")
		sparseMax   = flag.Int("sparse-max", 200, "max block density to count as sparse")
		limit       = flag.Int("limit", 500, "hard cap on probes per run (0 = no cap)")
		seed        = flag.Int64("seed", time.Now().UnixNano(), "RNG seed (estimate mode)")
		ua          = flag.String("ua", defaultUA, "User-Agent header")
		timeoutSec  = flag.Int("timeout", 20, "per-request timeout seconds")
		minBackoff  = flag.Int("backoff-min", 2, "initial backoff seconds on 429/5xx")
		maxBackoff  = flag.Int("backoff-max", 60, "max backoff seconds")
		maxRetries  = flag.Int("retries", 5, "max retries per request")
		dryRun      = flag.Bool("dry-run", false, "print candidates only, no probing")
		outPath     = flag.String("o", "", "write JSONL results to file (default stdout)")
		showVersion = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()
	if *showVersion {
		fmt.Println("issnprobe", version)
		return
	}

	if *cacheDir == "" {
		*cacheDir = filepath.Join(xdg.CacheHome, "issnprobe")
	}
	if err := os.MkdirAll(*cacheDir, 0o755); err != nil {
		log.Fatal(err)
	}

	pMin, err := strconv.Atoi(*prefixMin)
	if err != nil || pMin < 0 || pMin > 9999 {
		log.Fatalf("bad -prefix-min: %q", *prefixMin)
	}
	pMax, err := strconv.Atoi(*prefixMax)
	if err != nil || pMax < pMin || pMax > 9999 {
		log.Fatalf("bad -prefix-max: %q", *prefixMax)
	}

	log.Printf("loading known ISSN from %s", *issnPath)
	known, err := loadKnown(*issnPath)
	if err != nil {
		log.Fatalf("load %s: %v", *issnPath, err)
	}
	log.Printf("loaded %d known ISSN", len(known))

	density := make(map[string]int, 4000)
	for issn := range known {
		if len(issn) == 9 {
			density[issn[:4]]++
		}
	}

	spec := candidateSpec{
		mode:      *mode,
		prefixMin: pMin,
		prefixMax: pMax,
		sparseMax: *sparseMax,
		sampleN:   *sampleN,
		seed:      *seed,
	}
	candidates, err := buildCandidates(known, density, spec)
	if err != nil {
		log.Fatal(err)
	}
	poolSize := len(candidates)
	if *mode == "estimate" {
		// For estimate mode, the pool for N̂ is the full unknown pool
		// in [prefixMin..prefixMax], not just the sampled subset.
		poolSize = countUnknownPool(known, pMin, pMax)
	}
	if *mode != "estimate" {
		// stable ordering for sparse/frontier so runs are reproducible
		sort.Strings(candidates)
	}
	if *limit > 0 && len(candidates) > *limit {
		candidates = candidates[:*limit]
	}
	log.Printf("candidates to probe: %d (mode=%s)", len(candidates), *mode)

	if *dryRun {
		for _, c := range candidates {
			fmt.Println(c)
		}
		return
	}

	client := &http.Client{Timeout: time.Duration(*timeoutSec) * time.Second}
	prober := &Prober{
		client:     client,
		cacheDir:   *cacheDir,
		ua:         *ua,
		delay:      time.Duration(*delayMs) * time.Millisecond,
		saveBody:   *saveBody,
		minBackoff: time.Duration(*minBackoff) * time.Second,
		maxBackoff: time.Duration(*maxBackoff) * time.Second,
		maxRetries: *maxRetries,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var out io.Writer = os.Stdout
	if *outPath != "" {
		f, err := os.Create(*outPath)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		out = f
	}
	bw := bufio.NewWriter(out)
	defer bw.Flush()
	enc := json.NewEncoder(bw)

	var (
		probes, hits, cached, errs int
		lastNet                    time.Time
	)
	for _, issn := range candidates {
		if ctx.Err() != nil {
			break
		}
		_, hadCache := prober.readCache(issn)
		if !hadCache {
			// enforce min inter-request delay
			if !lastNet.IsZero() {
				elapsed := time.Since(lastNet)
				if elapsed < prober.delay {
					if err := sleepCtx(ctx, prober.delay-elapsed); err != nil {
						break
					}
				}
			}
			lastNet = time.Now()
		} else {
			cached++
		}
		r, err := prober.probe(ctx, issn)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				break
			}
			errs++
			log.Printf("probe %s: %v", issn, err)
			continue
		}
		probes++
		if r.Registered {
			hits++
		}
		if err := enc.Encode(r); err != nil {
			log.Printf("encode %s: %v", issn, err)
		}
	}
	bw.Flush()
	log.Printf("probes=%d hits=%d cached=%d errors=%d", probes, hits, cached, errs)
	if *mode == "estimate" && probes > 0 {
		e := computeEstimate(hits, probes, poolSize)
		b, _ := json.MarshalIndent(e, "", "  ")
		fmt.Fprintln(os.Stderr, string(b))
	}
}

func countUnknownPool(known map[string]struct{}, pMin, pMax int) int {
	count := 0
	for p := pMin; p <= pMax; p++ {
		prefix4 := fmt.Sprintf("%04d", p)
		for _, c := range blockCandidates(prefix4) {
			if _, ok := known[c]; !ok {
				count++
			}
		}
	}
	return count
}

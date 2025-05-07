// https://portal.issn.org/sitemap.xml
//
// Sitemap contains about 40 sub sitemaps, each with 50000 links. Cache all
// sitemaps, maybe versioned and generate list on demand from cache.
//
// Notes:
//
// Sometimes, a supposedly JSON response comes back as XML; it's weird and rare
// and I haven't been able to reproduce.
//
// The link https://portal.issn.org/resource/ISSN/0874-2308?format=json came
// back as 404, but it's there, right?
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/adrg/xdg"
	"github.com/miku/clam"
	"github.com/miku/issnlister/atomic"
	"github.com/miku/issnlister/lines"
	"github.com/miku/issnlister/stringutil"
	"github.com/miku/parallel"
	"github.com/sethgrid/pester"
	log "github.com/sirupsen/logrus"
	"github.com/vmihailenco/msgpack"
)

const (
	appName    = "issnlister"
	appVersion = "0.1.1"

	StatusOK   = "ok"
	StatusMiss = "xx"
)

var (
	defaultUserAgent = fmt.Sprintf("%s/%s (https://github.com/miku/issnlister)", appName, appVersion)

	sitemapIndex    = flag.String("s", "https://portal.issn.org/sitemap.xml", "the main sitemap")
	cacheDir        = flag.String("d", path.Join(xdg.CacheHome, appName), "path to cache dir")
	quiet           = flag.Bool("q", false, "suppress any extra output")
	list            = flag.Bool("l", false, "list all cached issn, one per line")
	dump            = flag.Bool("m", false, "download public metadata in JSON format")
	numWorkers      = flag.Int("w", runtime.NumCPU()*2, "number of workers")
	batchSize       = flag.Int("b", 100, "batch size per worker")
	ignoreFile      = flag.String("i", "", `path to file with ISSN to ignore, one ISSN (1234-575X) per line`)
	userAgent       = flag.String("ua", defaultUserAgent, "set user agent")
	showVersion     = flag.Bool("version", false, "show version")
	continueHarvest = flag.String("c", "", "continue harvest into a given file, implies -m")
	validate        = flag.Bool("k", false, "validate issn or list of issn (read from stdin)")
	cleanCache      = flag.Bool("C", false, "clean cache")
)

// Sitemapindex was generated 2019-09-28 18:56:12 by tir on sol.
type Sitemapindex struct {
	XMLName xml.Name `xml:"sitemapindex"`
	Text    string   `xml:",chardata"`
	Xmlns   string   `xml:"xmlns,attr"`
	Sitemap []struct {
		Text    string `xml:",chardata"`
		Loc     string `xml:"loc"`     // https://portal.issn.org/s...
		Lastmod string `xml:"lastmod"` // 2019-09-27, 2019-09-27, 2...
	} `xml:"sitemap"`
}

// Urlset was generated 2019-09-28 18:58:13 by tir on sol.
type Urlset struct {
	XMLName xml.Name `xml:"urlset"`
	Text    string   `xml:",chardata"`
	Xmlns   string   `xml:"xmlns,attr"`
	Xhtml   string   `xml:"xhtml,attr"`
	URL     []struct {
		Text string `xml:",chardata"`
		Loc  string `xml:"loc"` // https://portal.issn.org/r...
		Link []struct {
			Text     string `xml:",chardata"`
			Rel      string `xml:"rel,attr"`
			Hreflang string `xml:"hreflang,attr"`
			Href     string `xml:"href,attr"`
		} `xml:"link"`
		Lastmod    string `xml:"lastmod"`    // 2010-03-23, 2004-06-09, 2...
		Changefreq string `xml:"changefreq"` // monthly, monthly, monthly...
		Priority   string `xml:"priority"`   // 0.8, 0.8, 0.8, 0.8, 0.8, ...
	} `xml:"url"`
}

func main() {
	flag.Parse()
	if *showVersion {
		fmt.Printf("%s %s\n", appName, appVersion)
		os.Exit(0)
	}
	if *quiet {
		log.SetOutput(ioutil.Discard)
	}
	if *cleanCache {
		if _, err := os.Stat(*cacheDir); os.IsNotExist(err) {
			os.Exit(0)
		}
		size, err := DirSize(*cacheDir)
		if err != nil {
			log.Fatal(err)
		}
		if err := os.RemoveAll(*cacheDir); err != nil {
			log.Fatal(err)
		}
		fmt.Fprintf(os.Stderr, "issnlister: cleaned up %0.2fMB\n", float64(size)/1048576)
		os.Exit(0)
	}
	cacher := NewCacher()
	switch {
	case *list:
		issns, err := cacher.List()
		if err != nil {
			log.Fatal(err)
		}
		for _, issn := range issns {
			fmt.Println(issn)
		}
	case *validate:
		set, err := cacher.Set()
		if err != nil {
			log.Fatal(err)
		}
		stat, err := os.Stdin.Stat()
		if err != nil {
			log.Fatal(err)
		}
		// https://stackoverflow.com/a/26567513/89391
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			br := bufio.NewReader(os.Stdin)
			for {
				line, err := br.ReadString('\n')
				if err == io.EOF {
					break
				}
				if err != nil {
					log.Fatal(err)
				}
				v := strings.TrimSpace(line)
				if len(v) == 8 {
					v = v[:4] + "-" + v[4:]
				}
				_, ok := set[v]
				status := StatusOK
				if !ok {
					status = StatusMiss
				}
				fmt.Printf("%s\t%s\n", v, status)
			}
		} else {
			// Assume issnlister -c XXXX-XXXX ...
			for _, arg := range flag.Args() {
				v := strings.TrimSpace(arg)
				if len(v) == 8 {
					v = v[:4] + "-" + v[4:]
				}
				_, ok := set[v]
				status := StatusOK
				if !ok {
					status = StatusMiss
				}
				fmt.Printf("%s\t%s\n", v, status)
			}
		}

	case *continueHarvest != "":
		// -c implies -m
		*dump = true
		fallthrough
	case *dump:
		var output io.Writer = os.Stdout
		log.Printf("downloading public metadata")
		if *continueHarvest != "" {
			if *ignoreFile != "" {
				log.Fatal("use either -c or -i, not both (-c will generate an ignore file implicitly)")
			}
			var err error
			// Remove the last line from file, which might be partial. This is
			// inplace, but that's ok.
			if err = clam.Run(`touch "{{ previous }}" && sed -i '$ d' "{{ previous }}"`, clam.Map{"previous": *continueHarvest}); err != nil {
				log.Fatal(err)
			}
			// Find all already harvested ISSN and generate temporary ignore file, brittle regex.
			command := fmt.Sprintf(`LC_ALL=C grep -Eo '"@id":"resource/ISSN/[^"#]*' "{{ previous }}" | cut -d '/' -f 3 | LC_ALL=C sort -u > {{ output }}`)
			*ignoreFile, err = clam.RunOutput(command, clam.Map{"previous": *continueHarvest})
			if err != nil {
				log.Fatal(err)
			}
			defer func() {
				os.Remove(*ignoreFile)
			}()
			f, err := os.OpenFile(*continueHarvest, os.O_APPEND|os.O_RDWR|os.O_CREATE, 0644)
			if err != nil {
				log.Fatal(err)
			}
			defer f.Close()
			output = f
		}
		issns, err := cacher.List()
		if err != nil {
			log.Fatal(err)
		}
		if *ignoreFile != "" {
			ignoreList, err := lines.FromFile(*ignoreFile)
			if err != nil {
				log.Fatal(err)
			}
			log.Printf("%d to ignore", len(ignoreList))

			ignoreSet := stringutil.NewStringSet()
			for _, v := range ignoreList {
				ignoreSet.Add(v)
			}
			var filtered []string
			for _, issn := range issns {
				if ignoreSet.Contains(issn) {
					continue
				}
				filtered = append(filtered, issn)
			}
			log.Printf("started with %d issn", len(issns))
			issns = filtered
		}
		// Turn list of issn into list of links.
		// https://portal.issn.org/resource/ISSN/1521-9615?format=json
		links := make([]string, len(issns))
		for i := 0; i < len(links); i++ {
			links[i] = fmt.Sprintf("https://portal.issn.org/resource/ISSN/%s?format=json", issns[i])
		}
		log.Printf("attempting to download %d links", len(links))
		proc := parallel.NewProcessor(stringutil.SliceReader(links), output, fetch)
		proc.BatchSize = *batchSize
		proc.NumWorkers = *numWorkers
		if err := proc.Run(); err != nil {
			log.Fatal(err)
		}
	}
}

func DirSize(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return err
	})
	return size, err
}

// Cacher fetches and caches responses.
type Cacher struct {
	Directory string
	Prefix    string
	Locs      []string // Sitemap locations.
}

// NewCacher returns a Cacher with a default prefix (changing per day).
func NewCacher() *Cacher {
	return &Cacher{
		Directory: *cacheDir,
		Prefix:    time.Now().Format("2006-01-02"),
	}
}

// SitemapDir returns the directory to cache the sitemap.
func (c *Cacher) SitemapDir() string {
	return filepath.Join(c.Directory, c.Prefix)
}

// SitemapFile returns the filename for the global sitemap and creates
// necessary subdirectories to hold the file.
func (c *Cacher) SitemapFile() string {
	return filepath.Join(c.SitemapDir(), "sitemap.xml")
}

// SerialnumbersFile return the location of the issn list cache file.
func (c *Cacher) SerialnumbersFile() string {
	return filepath.Join(c.SitemapDir(), "issnlist.tsv")
}

// SerialnumbersSetFile returns the filename of the serialized set of issns.
func (c *Cacher) SerialnumbersSetFile() string {
	return filepath.Join(c.SitemapDir(), "issns.msgp")
}

// fetchSitemapIndex downloads and caches the main sitemap file.
func (c *Cacher) fetchSitemapIndex() error {
	if err := ensureDir(c.SitemapDir()); err != nil {
		return err
	}
	if _, err := os.Stat(c.SitemapFile()); err == nil {
		return nil
	}
	resp, err := pester.Get(*sitemapIndex)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("failed to fetch sitemap: %d", resp.StatusCode)
	}
	defer resp.Body.Close()
	return atomic.WriteFileReader(c.SitemapFile(), resp.Body, 0644)
}

// fetchSitemaps tries to fetch all linked sitemaps (currently around 40).
func (c *Cacher) fetchSitemaps() error {
	if err := c.findLocations(); err != nil {
		return err
	}
	for _, loc := range c.Locs {
		// https://portal.issn.org/sitemap6.xml
		parts := strings.Split(loc, "/")
		filename := filepath.Join(c.SitemapDir(), parts[len(parts)-1])
		if _, err := os.Stat(filename); err == nil {
			log.Printf("%s cached at %s", loc, filename)
			continue
		}
		log.Println(loc)
		resp, err := pester.Get(loc)
		if err != nil {
			return err
		}
		if resp.StatusCode >= 400 {
			return fmt.Errorf("failed to fetch sitemap at %s: %d", loc, resp.StatusCode)
		}
		defer resp.Body.Close()
		if err := atomic.WriteFileReader(filename, resp.Body, 0644); err != nil {
			return err
		}
	}
	return nil
}

// findLocations populates the linked sitemaps.
func (c *Cacher) findLocations() error {
	if err := c.fetchSitemapIndex(); err != nil {
		return err
	}
	f, err := os.Open(c.SitemapFile())
	if err != nil {
		return err
	}
	defer f.Close()
	dec := xml.NewDecoder(f)
	var si Sitemapindex
	if err := dec.Decode(&si); err != nil {
		return err
	}
	for _, sm := range si.Sitemap {
		c.Locs = append(c.Locs, sm.Loc)
	}
	return nil
}

// Set returns a set of ISSN, cached for performance.
func (c *Cacher) Set() (map[string]struct{}, error) {
	if _, err := os.Stat(c.SerialnumbersSetFile()); err != nil {
		log.Printf("caching set at %v", c.SerialnumbersSetFile())
		list, err := c.List()
		if err != nil {
			return nil, err
		}
		m := make(map[string]struct{})
		for _, issn := range list {
			m[issn] = struct{}{}
		}
		b, err := msgpack.Marshal(m)
		if err != nil {
			return nil, err
		}
		if err := ioutil.WriteFile(c.SerialnumbersSetFile(), b, 0644); err != nil {
			return nil, err
		}
	}
	b, err := ioutil.ReadFile(c.SerialnumbersSetFile())
	if err != nil {
		return nil, err
	}
	v := make(map[string]struct{})
	if err := msgpack.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	return v, nil
}

// List returns a string slice of all ISSN.
func (c *Cacher) List() ([]string, error) {
	if _, err := os.Stat(c.SerialnumbersFile()); err == nil {
		f, err := os.Open(c.SerialnumbersFile())
		if err != nil {
			return nil, err
		}
		defer f.Close()
		b, err := ioutil.ReadFile(c.SerialnumbersFile())
		return strings.Split(string(b), "\n"), nil
	}
	if err := c.fetchSitemaps(); err != nil {
		return nil, err
	}
	files, err := ioutil.ReadDir(c.SitemapDir())
	if err != nil {
		return nil, err
	}

	// Input buffer, filenames, one per line.
	var buf bytes.Buffer
	for _, fi := range files {
		if fi.Name() == "sitemap.xml" {
			continue
		}
		filename := filepath.Join(c.SitemapDir(), fi.Name())
		io.WriteString(&buf, filename+"\n")
	}

	// Write one issn per line into output buffer.
	var output bytes.Buffer

	processor := parallel.NewProcessor(&buf, &output, func(b []byte) ([]byte, error) {
		filename := strings.TrimSpace(string(b))
		f, err := os.Open(filename)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		dec := xml.NewDecoder(f)
		var us Urlset
		if err := dec.Decode(&us); err != nil {
			return nil, err
		}
		var buf bytes.Buffer
		for _, u := range us.URL {
			parts := strings.Split(u.Loc, "/")
			issn := parts[len(parts)-1]
			io.WriteString(&buf, issn+"\n")
		}
		return buf.Bytes(), nil
	})

	// Give each worker two files at a time.
	processor.BatchSize = 2
	if err := processor.Run(); err != nil {
		return nil, err
	}

	var result []string
	for _, v := range strings.Split(output.String(), "\n") {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		result = append(result, v)
	}
	sort.Strings(result)
	if err := atomic.WriteFile(c.SerialnumbersFile(), []byte(strings.Join(result, "\n")), 0644); err != nil {
		return nil, err
	}
	return result, nil
}

// ensureDir makes sure a directory exists.
func ensureDir(name string) error {
	if _, err := os.Stat(name); os.IsNotExist(err) {
		if err := os.MkdirAll(name, 0755); err != nil {
			return err
			log.Fatal(err)
		} else {
			log.Printf("created directory at: %s", name)
		}
	}
	return nil
}

// fetch can be plugged into miku/parallel for parallel processing. TODO(miku):
// Make parallel a bit simpler to use outside the reader/writer realm. The byte
// slices contains a list of issn, separated by newline.
func fetch(b []byte) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	client := pester.New()
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var (
			retryCount = 10
			errors     []string
		)
		for {
			if retryCount == 0 {
				return nil, fmt.Errorf("giving up on %s, errors were: %v", line, errors)
			}
			retryCount--

			req, err := http.NewRequest("GET", line, nil)
			if err != nil {
				return nil, err
			}
			req.Header.Add("User-Agent", *userAgent)
			resp, err := client.Do(req)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()
			if resp.StatusCode >= 400 {
				msg := fmt.Sprintf("got %s on %s", resp.Status, line)
				errors = append(errors, msg)
				log.Warn(msg)
				continue
			}
			var body bytes.Buffer
			tee := io.TeeReader(resp.Body, &body)
			// Just a container to hold the data to serialize (compact) again.
			var m = make(map[string]interface{})
			if err := json.NewDecoder(tee).Decode(&m); err != nil {
				log.Printf("%v at %s", err, line)
				log.Println(body.String())
				msg := fmt.Sprintf("%s failed with %s [%d]", line, err, retryCount)
				errors = append(errors, msg)
				log.Warn(msg)
				continue
			}
			if err := enc.Encode(m); err != nil {
				return nil, err
			}
			break
		}
	}
	return buf.Bytes(), nil
}

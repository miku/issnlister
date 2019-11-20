// https://portal.issn.org/sitemap.xml
//
// Sitemap contains about 40 sub sitemaps, each with 50000 links. Cache all
// sitemaps, maybe versioned and generate list on demand from cache.
package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/adrg/xdg"
	"github.com/miku/parallel"
	"github.com/sethgrid/pester"
	log "github.com/sirupsen/logrus"
)

const appName = "issnlister"

var (
	sitemapIndex = flag.String("s", "https://portal.issn.org/sitemap.xml", "the main sitemap")
	cacheDir     = flag.String("d", path.Join(xdg.CacheHome, appName), "path to cache dir")
	quiet        = flag.Bool("q", false, "suppress any extra output")
	list         = flag.Bool("l", false, "list all cached issn, one per line")
	dump         = flag.Bool("m", false, "download public metadata in JSON format")
	numWorkers   = flag.Int("w", runtime.NumCPU()*2, "number of workers")
	batchSize    = flag.Int("b", 2000, "batch size per worker")
)

func WriteFileAtomicReader(filename string, r io.Reader, perm os.FileMode) error {
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return err
	}
	return WriteFileAtomic(filename, b, perm)
}

// WriteFileAtomic writes the data to a temp file and atomically move if everything else succeeds.
func WriteFileAtomic(filename string, data []byte, perm os.FileMode) error {
	dir, name := path.Split(filename)
	f, err := ioutil.TempFile(dir, name)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if permErr := os.Chmod(f.Name(), perm); err == nil {
		err = permErr
	}
	if err == nil {
		err = os.Rename(f.Name(), filename)
	}
	// Any err should result in full cleanup.
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}

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

// Cacher fetches and caches responses.
type Cacher struct {
	Directory string
	Prefix    string
	Locs      []string // Sitemap locations.
}

func NewCacher() *Cacher {
	return &Cacher{
		Directory: *cacheDir,
		Prefix:    time.Now().Format("2006-01-02"),
	}
}

func (c *Cacher) SitemapDir() string {
	return filepath.Join(c.Directory, c.Prefix)
}

// SitemapFile returns the filename and creates necessary subdirectories to
// hold the file.
func (c *Cacher) SitemapFile() string {
	return filepath.Join(c.SitemapDir(), "sitemap.xml")
}

func (c *Cacher) SerialnumbersFile() string {
	return filepath.Join(c.SitemapDir(), "issnlist.tsv")
}

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
	return WriteFileAtomicReader(c.SitemapFile(), resp.Body, 0644)
}

// FetchSitemaps tries to fetch all sitemaps.
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
		if err := WriteFileAtomicReader(filename, resp.Body, 0644); err != nil {
			return err
		}
	}
	return nil
}

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

// List returns a list of ISSN.
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

	// Give each worker two files at a file.
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
	if err := WriteFileAtomic(c.SerialnumbersFile(), []byte(strings.Join(result, "\n")), 0644); err != nil {
		return nil, err
	}
	return result, nil
}

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
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		resp, err := pester.Get(line)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("got %d %s on %s", resp.StatusCode, resp.Status, line)
		}
		// Just a minimal container to hold the data to serialize (compact) again.
		var m = make(map[string]interface{})
		if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
			return nil, err
		}
		if err := enc.Encode(m); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func SliceReader(s []string) io.Reader {
	return strings.NewReader(strings.Join(s, "\n"))
}

func main() {
	flag.Parse()
	if *quiet {
		log.SetOutput(ioutil.Discard)
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
	case *dump:
		log.Printf("downloading public metadata")
		issns, err := cacher.List()
		if err != nil {
			log.Fatal(err)
		}
		// Turn list of issn into list of links.
		// https://portal.issn.org/resource/ISSN/1521-9615?format=json
		links := make([]string, len(issns))
		for i := 0; i < len(links); i++ {
			links[i] = fmt.Sprintf("https://portal.issn.org/resource/ISSN/%s?format=json", issns[i])
		}
		log.Printf("attempting to download %d links", len(links))
		proc := parallel.NewProcessor(SliceReader(links), os.Stdout, fetch)
		proc.BatchSize = *batchSize
		proc.NumWorkers = *numWorkers
		if err := proc.Run(); err != nil {
			log.Fatal(err)
		}
	}
}

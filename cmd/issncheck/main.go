// issncheck tells you, whether an ISSN is registered or not (by using a
// hopefully up to date list of ISSN scraped from issn.org sitemap).
//
// Note: The issn.tsv file will be temporarily copied into this folder during
// compilation.
package main

import (
	"bufio"
	_ "embed"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
)

//go:embed issn.tsv
var issnlist string

var issnMap = make(map[string]struct{})

func main() {
	for _, v := range strings.Split(issnlist, "\n") {
		issnMap[v] = struct{}{}
	}
	br := bufio.NewReader(os.Stdin)
	bw := bufio.NewWriter(os.Stdout)
	defer bw.Flush()
	for {
		line, err := br.ReadString('\n')
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		line = strings.TrimSpace(line)
		line = strings.ReplaceAll(line, " ", "")
		line = strings.ReplaceAll(line, "-", "")
		if len(line) != 8 {
			fmt.Printf("X\t%v\n", line)
			continue
		}
		v := line[:4] + "-" + line[4:]
		if _, ok := issnMap[v]; ok {
			fmt.Fprintf(bw, "1\t%v\n", v)
		} else {
			fmt.Fprintf(bw, "0\t%v\n", v)
		}
	}
}

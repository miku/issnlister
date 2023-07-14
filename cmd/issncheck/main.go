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
			fmt.Printf("1\t%v\n", v)
		} else {
			fmt.Printf("0\t%v\n", v)
		}
	}
}

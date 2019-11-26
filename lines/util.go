package lines

import (
	"bufio"
	"io"
	"os"
	"strings"
)

// FromReader splits data on newlines and all lines as a slice of strings.
func FromReader(r io.Reader) (result []string, err error) {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadString('\n')
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		result = append(result, strings.TrimSpace(line))
	}
	return result, nil
}

// FromFile takes a filename and returns the lines in the file as a string slice.
func FromFile(filename string) ([]string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return FromReader(f)
}

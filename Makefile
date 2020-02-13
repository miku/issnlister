SHELL := /bin/bash
TARGETS := issnlister

.PHONY: all
all: $(TARGETS)

%: cmd/%/main.go
	go build -o $@ $<

.PHONY: clean
clean:
	rm -f issn.tsv
	rm -f issnlister

issn.tsv: all
	./issnlister -l | sort -u > $@

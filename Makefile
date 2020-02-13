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
	sed -i -e "s/ISSN-LIST-DATE: [0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]/ISSN-LIST-DATE: $$(date +'%Y-%m-%d')/g" README.md
	sed -i -e "s/COUNT: [0-9]*/COUNT: $$(wc -l $@ | awk '{print $$1}')/g" README.md


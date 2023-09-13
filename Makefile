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
	rm -f issncheck
	rm -fr __pycache__

issn.tsv: all
	./issnlister -l | sort -S50% -u > $@
	sed -i -e "s/ISSN-LIST-DATE: [0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]/ISSN-LIST-DATE: $$(date +'%Y-%m-%d')/g" README.md
	sed -i -e "s/COUNT: [0-9]*/COUNT: $$(wc -l $@ | awk '{print $$1}')/g" README.md

issn.py: issn.tsv
	bash issn.py.gen > issn.py

issncheck: cmd/issncheck/main.go issn.tsv
	cp issn.tsv cmd/issncheck/issn.tsv
	go build -o issncheck cmd/issncheck/main.go
	rm -f cmd/issncheck/issn.tsv


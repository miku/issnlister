issnlister: issnlister.go
	go build -o $@ $<

.PHONY: clean
clean:
	rm -f issnlister


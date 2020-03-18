# ISSN public data dump

```
$ issnlister -version
issnlister 0.1.1

$ issnlister -c data.ndjson # might take a day (restartable)
```

# ISSN lists

```
$ python make_issn_issnl_mapping.py data.ndjson > 20200318.ISSN-to-ISSN-L.txt
$ awk '{print $2"\t"$1}' 20200318.ISSN-to-ISSN-L.txt > 20200318.ISSNL-to-ISSN.txt

$ wc -l 20* data.ndjson
   2140743 20200318.ISSNL-to-ISSN.txt
   2140743 20200318.ISSN-to-ISSN-L.txt
   2141737 data.ndjson
```

# SHA1

```
$ sha1sum *
f668b84be0673643ba700f6c9a8c1aa34e8fb333  20200318.ISSNL-to-ISSN.txt
6f332d9e71b1e0b330832fe6501844ef0ba5adf8  20200318.ISSN-to-ISSN-L.txt
bf95b7d0fbc9666e197e28dec26b0cc726227c16  data.ndjson
059c0726d67f166e0e15a85f1f53662f85d9980d  data.ndjson.xz
a0de8029edb2e5361e1f545c6b8b590c7f2cf210  issn.jpg
```

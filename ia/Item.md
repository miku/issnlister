# Upload to IA

* identifier: issn_public_data_20191125

```
$ ls -la data.ndjson
-rw-r--r-- 1 tir tir 6637105024 Dec  9 18:57 data.ndjson

$ LC_ALL=C wc -l data.ndjson
2123396 data.ndjson

$ sha1sum data.ndjson
2c94898d2087e1485855cc6f53d44390cbe4eabc  data.ndjson

$ sha256sum data.ndjson
e1f20bd7ea4b8c40ba349e5fa8075a2c4ceea03c14ae9447e5638630dbdf5380  data.ndjson

$ sha1sum data.ndjson.xz
c1e4e1ea49ec47d8221e5cd18b94b5ef44454185  data.ndjson.xz

```

Command:

```
$ ia upload issn_public_data_20191125 issn.jpg data.ndjson.xz --metadata="mediatype:data" --metadata="collection:ia_biblio_metadata" --retries 3
```

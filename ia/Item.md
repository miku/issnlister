# Upload to IA

* identifier: issn_public_data_20191125

```
$ ls -la data.ndj
-rw-r--r-- 1 tir tir 6637105024 Dec  9 18:57 data.ndj

$ LC_ALL=C wc -l data.ndj
2123396 data.ndj

$ sha1sum data.ndj
2c94898d2087e1485855cc6f53d44390cbe4eabc  data.ndj

$ sha256sum data.ndj
e1f20bd7ea4b8c40ba349e5fa8075a2c4ceea03c14ae9447e5638630dbdf5380  data.ndj
```

Command:

```
$ ia upload issn_public_data_20191125 issn.jpg data.ndj --metadata="mediatype:data" --metadata="collection:ia_biblio_metadata" --retries 3
```

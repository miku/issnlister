#!/usr/bin/env python
#
# Related: https://archive.org/details/issn_issnl_mappings
#
# To create a ISSN ISSNL list:
#
#     $ issnlister -c data.ndj
#     $ python make_issn_issnl_mapping.py data.ndj > issn_issnl_mapping.tsv
#
# To reverse:
#
#     $ issnlister -c data.ndj
#     $ python make_issn_issnl_mapping.py data.ndj | awk '{print $2"\t"$1}' > issnl_issn_mapping.tsv

import json
import sys

if __name__ == '__main__':
    if len(sys.argv) == 1:
        raise ValueError("usage: %s FILE" % sys.argv[0])

    with open(sys.argv[1]) as handle:
        for line in handle:
            line = line.strip()
            try:
                doc = json.loads(line)
            except json.decoder.JSONDecodeError as err:
                print(err, file=sys.stderr)
                continue
            result = {}
            if not '@graph' in doc:
                print(doc, file=sys.stderr)
                continue
            for item in doc.get('@graph'):
                if item['@id'].startswith('resource/ISSN-L/'):
                    result.update({'issnl': item['@id'].split('/')[-1]})
                if item['@id'].startswith('resource/ISSN/'):
                    result.update({'issn': item['@id'].split('#')[0].split('/')[-1]})

            print(result['issn'] + "\t" + result.get('issnl', ''))


#!/usr/bin/env python

"""
A minimal script to check, whether an ISSN is valid.

    $ python validateissn.py 1234-5678
    1234-5678       False

    $ python validateissn.py 12345679
    1234-5679       True

Also calculate check digit:

    $ python validateissn.py 4444222
    4444222 4444-222X

    $ python validateissn.py 4444-222
    4444222 4444-222X
"""

import argparse
import sys

def calculate_issn_checkdigit(s):
    """
    Given a string of length 7, return the ISSN check digit.
    """
    if len(s) != 7:
        raise ValueError('seven digits required')
    ss = sum([int(digit) * f for digit, f in zip(s, range(8, 1, -1))])
    _, mod = divmod(ss, 11)
    checkdigit = 0 if mod == 0 else 11 - mod
    if checkdigit == 10:
        checkdigit = 'X'
    return '{}'.format(checkdigit)

def validate(issn):
    if '-' in issn:
        issn = issn.replace('-', '')
    if len(issn) != 8:
        raise ValueError('invalid issn: {}'.format(issn))
    checkdigit = calculate_issn_checkdigit(issn[:7])
    return issn[7] == '{}'.format(checkdigit)

def normalize(issn):
    issn = issn.upper()
    if len(issn) == 8:
        return issn[:4] + "-" + issn[4:]
    elif len(issn) == 9 and issn[4] == '-':
        return issn
    raise ValueError('cannot normalize: %s' % issn)


if __name__ == '__main__':
    parser = argparse.ArgumentParser('validateissn.py')
    parser.add_argument('issns', metavar='issns', nargs='*')
    args = parser.parse_args()

    if len(args.issns) > 0:
        issns = args.issns
    else:
        issns = [
            ('2347-6710'),
            ('0378-5955'),
            ('0003-200X'),
            ('0003-5661'),
            ('0003-5660'),
        ]
    try:
        for issn in issns:
            v = issn.replace('-', '')
            if len(v) == 7:
                print('{}\t{}'.format(issn, normalize(v + calculate_issn_checkdigit(v))))
            else:
                print('{}\t{}'.format(normalize(issn), validate(issn)))
    except ValueError as err:
        print(err, file=sys.stdout)


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
            if len(issn.replace('-', '')) == 7:
                print('{}\t{}'.format(issn, issn + calculate_issn_checkdigit(issn)))
            else:
                print('{}\t{}'.format(issn, validate(issn)))
    except ValueError as err:
        print(err, file=sys.stdout)


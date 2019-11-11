# ISSN list

[International Standard Serial
Number](https://en.wikipedia.org/wiki/International_Standard_Serial_Number), is
an eight-digit serial number used to uniquely identify a serial publication,
such as a magazine.

Issuing organisation: [issn.org](http://www.issn.org/).

> The CIEPS, also known as the ISSN International Centre, is an
intergovernmental organization which manages at the international level the
identification and the description of serial publications and ongoing
resources, print and online, in any subject.

## Variants

* E-ISSN (electronic), P-ISSN (print), ISSN-L (link)

> Conversely, as defined in ISO 3297:2007, every serial in the ISSN system is
also assigned a linking ISSN (ISSN-L), typically the same as the ISSN assigned
to the serial in its first published medium, which links together all ISSNs
assigned to the serial in every medium

## Basic validation

```python
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
```

## Number of ISSN

* 2714711 (as of 2019-11-11)

## Upper limit of valid ISSN?

* 10^7

Current probability that a random, valid ISSN is registered: 0.2714711.

## Distribution

Snapshot, 2019-11-11, 15:00, UTC+1.

![](static/map.png)



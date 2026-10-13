#!/usr/bin/env python3
"""Builds internal/discover/english_words.txt, the word list the Deep Random
windows use to tell a clean scan from a dirty one and to mend small OCR
mistakes (internal/discover/clean.go).

The list is the words that turn up in at least MIN_BOOKS of a spread of public
domain books from Project Gutenberg (the most downloaded English books, plus
science fiction, adventure and periodicals, so that the vocabulary of pulps and
magazines is in it), so that names belonging to one book are left out and the
ordinary vocabulary, with its inflections, stays.

The books are named in scripts/english_words_books.txt (one Gutenberg number
per line). `--find` rebuilds that file from Gutendex; plain runs read it.

    python3 -I scripts/english_words.py --find
    python3 -I scripts/english_words.py
"""
import collections
import json
import os
import re
import sys
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
BOOKS = os.path.join(HERE, "english_words_books.txt")
OUT = os.path.join(HERE, "..", "internal", "discover", "english_words.txt")
MIN_BOOKS = 6
MIN_TOTAL = 10
UA = {"User-Agent": "w5f-wordlist/1"}


def get(url):
    req = urllib.request.Request(url, headers=UA)
    return urllib.request.urlopen(req, timeout=30).read()


def find():
    ids = []
    queries = [("", 8), ("&topic=science%20fiction", 4), ("&topic=adventure", 3), ("&topic=periodicals", 2),
               ("&topic=detective", 3), ("&topic=western", 3), ("&topic=history", 2), ("&topic=cooking", 1),
               ("&topic=technology", 2), ("&topic=humor", 2)]
    for extra, pages in queries:
        for page in range(1, pages + 1):
            try:
                data = json.loads(get(f"https://gutendex.com/books/?languages=en&sort=popular&page={page}{extra}"))
            except Exception as e:
                print("skip", extra, page, e, file=sys.stderr)
                break
            for b in data["results"]:
                if b["id"] not in ids:
                    ids.append(b["id"])
    with open(BOOKS, "w", newline="\n") as f:
        f.write("\n".join(str(i) for i in ids) + "\n")
    print(len(ids), "books ->", BOOKS)


def build():
    word = re.compile(r"[a-z]+(?:'[a-z]+)?")
    books = collections.defaultdict(set)
    total = collections.Counter()
    used = 0
    for n in (int(x) for x in open(BOOKS).read().split()):
        try:
            text = get(f"https://www.gutenberg.org/cache/epub/{n}/pg{n}.txt").decode("utf-8", "replace")
        except Exception as e:  # a missing book only makes the list a little smaller
            print("skip", n, e, file=sys.stderr)
            continue
        used += 1
        start = text.find("*** START")
        end = text.rfind("*** END")
        if start >= 0 and end > start:
            text = text[start:end]
        for w in word.findall(text.lower().replace("’", "'")):
            books[w].add(n)
            total[w] += 1
    words = sorted(w for w in books if len(w) >= 3 and len(books[w]) >= MIN_BOOKS and total[w] >= MIN_TOTAL)
    with open(OUT, "w", encoding="utf-8", newline="\n") as f:
        f.write("\n".join(words) + "\n")
    print(f"{used} books, {len(words)} words -> {OUT}")


if "--find" in sys.argv:
    find()
else:
    build()

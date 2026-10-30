#!/usr/bin/env python3
"""Regenerate internal/discover/bhl_parts.tsv.gz from BHL's open data (CC0).

    python scripts/bhl_parts.py [data_dir]

Downloads data/{part,partpage}.txt.gz from the public bhl-open-data S3 bucket
(about 50 MB) into data_dir (default: a temp folder) and writes the whole
articles Deep random's "nature" family draws from (the owner, 2026-10-30: a
whole article, not leaves from the middle of a book). An article is kept when
it is English, three to 25 pages long, not a handwritten letter, and its title
is that of something to read: not a description of new species, a list, a
catalogue, a report or a revision, and in English, not Latin. Its rights are
not looked at (the owner, 2026-10-30): whatever BHL shows is drawn.

Each line: part, item, year, first page id, pages, title, container. The text
of an article is its item's OCR leaves from the first page on (the part's own
OCR folder is empty for most).
"""
import csv
import gzip
import os
import re
import sys
import tempfile
import urllib.request

BUCKET = "https://bhl-open-data.s3.us-east-2.amazonaws.com/data/"
HERE = os.path.dirname(os.path.abspath(__file__))
csv.field_size_limit(10**9)

NOT_READING = re.compile(
    r"new (species|genus|genera|forms?|variet|fish|bird|plant|insect|shell)|\bgen(us|era)\b|\bsp(ecies|p)?\.? nov|"
    r"descri(ption|ptive|bed)|diagnos|list of|lists?\b|check-?list|catalogu?e|synops|revision|revised|monograph|"
    r"key to|^notes?\b|\bnotes? (on|upon|from|and|for)\b|\bnews\b|ratings|contributions?|additions?|records?|report|proceedings|minutes|meeting|index|"
    r"errata|obituar|review|bibliograph|collected|collection|accessions|exhibitions?|notices|current literature|"
    r"correspondence|letters|announcements|miscellane|abstracts?|plates?\b|explanation|figures|preliminary|supplement|"
    r"\b(a|an|some|several|two|three|four|on) new\b|\bspecies\b|"
    r"\b[A-Z][a-z]+(idae|inae|aceae|oidea|ella|ia)\b",
    re.I)
# Collections of letters are handwriting: their OCR does not read.
LETTERS = re.compile(r"correspondence|letters", re.I)


def fetch(name, folder):
    path = os.path.join(folder, name)
    if not os.path.exists(path):
        urllib.request.urlretrieve(BUCKET + name, path)
    return path


def rows(path):
    with gzip.open(path, "rt", encoding="utf-8-sig", newline="") as f:
        r = csv.reader(f, delimiter="\t")
        head = next(r)
        for row in r:
            yield dict(zip(head, row))


def english(title, known):
    words = [w.lower() for w in re.findall(r"[A-Za-z]+", title) if len(w) > 2]
    return len(words) >= 2 and sum(w in known or w.rstrip("s") in known for w in words) >= 0.7 * len(words)


def main():
    folder = sys.argv[1] if len(sys.argv) > 1 else tempfile.mkdtemp(prefix="bhl")
    os.makedirs(folder, exist_ok=True)
    part, partpage = (fetch(n + ".txt.gz", folder) for n in ("part", "partpage"))
    with open(os.path.join(HERE, "..", "internal", "discover", "english_words.txt"), encoding="utf-8") as f:
        known = {w.strip() for w in f if w.strip()}

    keep = {}
    for r in rows(part):
        if r["LanguageName"] != "English" or LETTERS.search(r["ContainerTitle"]) or not r["ItemID"].isdigit():
            continue
        m = re.search(r"(1[5-9]\d\d|20\d\d)", r["Date"] or r["PublicationDetails"])
        year = int(m.group(1)) if m else 0
        title = re.sub(r"\s+", " ", r["Title"]).replace("\t", " ").strip()
        title = re.sub(r"^([IVXLC]+|\d+)\.\s*[—-]?\s*", "", title)  # "XL.—Observations on…"
        if len(title) < 8 or NOT_READING.search(title) or not english(title, known):
            continue
        keep[r["PartID"]] = (r["ItemID"], year, title[:110],
                             re.sub(r"\s+", " ", r["ContainerTitle"]).replace("\t", " ").strip()[:80])

    pages = {}
    for r in rows(partpage):
        if r["PartID"] in keep:
            pages.setdefault(r["PartID"], []).append((int(r["SequenceOrder"] or 0), int(r["PageID"])))

    out = []
    for pid, (item, year, title, container) in keep.items():
        ps = sorted(pages.get(pid, []))
        if not 3 <= len(ps) <= 25:
            continue
        out.append((int(pid), int(item), year, ps[0][1], len(ps), title, container))
    out.sort()
    target = os.path.join(HERE, "..", "internal", "discover", "bhl_parts.tsv.gz")
    with gzip.open(target, "wt", encoding="utf-8", newline="\n") as f:
        f.write("# part\titem\tyear\tfirst page\tpages\ttitle\tcontainer (BHL open data, CC0; regenerate with scripts/bhl_parts.py)\n")
        for o in out:
            f.write("%d\t%d\t%d\t%d\t%d\t%s\t%s\n" % o)
    print(len(out), "articles ->", os.path.normpath(target), os.path.getsize(target), "bytes")


if __name__ == "__main__":
    main()

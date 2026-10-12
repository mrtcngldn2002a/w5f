#!/usr/bin/env python3
"""Regenerate internal/discover/bhl_curious.tsv from BHL's open data (CC0).

    python scripts/bhl_index.py [data_dir]

Downloads data/{subject,title,item}.txt.gz from the public bhl-open-data S3
bucket (about 42 MB) into data_dir (default: a temp folder) and writes the list
Deep random's "nature" family draws from. Tier 1 holds items on curious
subjects (bestiaries, monsters, folklore, herbals, cabinets of curiosities...),
tier 2 natural history and travel books up to 1910. English only, up to 1923.
"""
import csv
import gzip
import os
import re
import sys
import tempfile
import urllib.request

BUCKET = "https://bhl-open-data.s3.us-east-2.amazonaws.com/data/"
csv.field_size_limit(10**9)

CURIOUS = re.compile(
    r"bestiar|monster|dragons?$|\bserpents?\b|sea.serpent|mermaid|unicorn|cryptozoo|folk-?lore|superstition|"
    r"mytholog|legend|magic|alchem|witch|herbals?$|curiosit|ghost|demon|occult|supernatur|cabinets? of|fabulous|"
    r"animals, (legends|mythical)|serpent worship|astrolog|zoology, medical|medicine, herbal|herbalism|"
    r"religion and mythology|poisonous (plants|animals|snakes)")
BROAD = re.compile(r"^(natural history|description and travel|voyages and travels|voyages around the world|"
                   r"discovery and exploration|travel literature|travel)$")
# Serials, reports and reference works are for looking things up, not reading.
NOISE = re.compile(r"bulletin|proceedings|report|journal|transactions|annual|memoirs|catalog|bibliograph|index|"
                   r"records of|occasional papers|contributions|publications|annals|museum|society|survey|circular|"
                   r"notes from|leaflet|abstract|statistics|handbook|manual|list of", re.I)


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


def main():
    folder = sys.argv[1] if len(sys.argv) > 1 else tempfile.mkdtemp(prefix="bhl")
    os.makedirs(folder, exist_ok=True)
    subject, title, item = (fetch(n + ".txt.gz", folder) for n in ("subject", "title", "item"))
    tier = {}
    for r in rows(subject):
        s, t = r["Subject"].strip().lower(), r["TitleID"]
        if CURIOUS.search(s):
            tier[t] = 1
        elif BROAD.match(s) and t not in tier:
            tier[t] = 2
    titles = {r["TitleID"]: r for r in rows(title) if r["TitleID"] in tier}
    out = []
    for r in rows(item):
        t = r["TitleID"]
        if t not in tier or t not in titles or not r.get("BarCode"):
            continue
        ti = titles[t]
        if ti.get("LanguageCode", "") != "ENG":
            continue
        m = re.match(r"\d{4}", r.get("Year", ""))
        year = int(m.group(0)) if m else 0
        if year > 1923:
            continue
        name = re.sub(r"\s+", " ", ti["FullTitle"] or ti["ShortTitle"]).replace("\t", " ").strip()
        if NOISE.search(name) and (tier[t] == 2 or re.search(r"index|catalog", name, re.I)):
            continue
        if tier[t] == 2 and (year == 0 or year > 1910):
            continue
        out.append((tier[t], int(r["ItemID"]), year, name[:110]))
    out.sort(key=lambda o: o[1])
    target = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "internal", "discover", "bhl_curious.tsv")
    with open(target, "w", encoding="utf-8", newline="\n") as f:
        f.write("# tier\titem\tyear\ttitle (BHL open data, CC0; regenerate with scripts/bhl_index.py)\n")
        for o in out:
            f.write("%d\t%d\t%d\t%s\n" % o)
    print(len(out), "items ->", os.path.normpath(target))


if __name__ == "__main__":
    main()

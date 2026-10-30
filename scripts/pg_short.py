#!/usr/bin/env python3
"""Regenerate internal/discover/pg_short.tsv.gz from Project Gutenberg's catalogue.

    python scripts/pg_short.py [data_dir]

Downloads cache/epub/feeds/rdf-files.tar.bz2 (about 125 MB, the catalogue
with every file's size) into data_dir (default: a temp folder) and writes the
English books Deep random's "print" shelf draws from (the owner, 2026-10-30:
short whole works only, no novels; the stories of anthologies and magazine
issues one at a time):

    s  a single story: science fiction, fantasy, horror, a short story
    e  a single essay
    i  an issue of an old magazine (Astounding Stories, The Atlantic Monthly,
       Blackwood's...): W5F opens one of its stories or articles
    a  an anthology of stories: W5F opens one of them

A single work is 1,000 to 20,000 words long (its size less Gutenberg's
licence, at six bytes a word); a book whose title says it is a collection is
an anthology instead. Children's books, plays and poetry are left out.

Each line: book, kind, words, title, author.
"""
import gzip
import os
import re
import sys
import tarfile
import tempfile
import urllib.request
import xml.etree.ElementTree as ET

FEED = "https://www.gutenberg.org/cache/epub/feeds/rdf-files.tar.bz2"
HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.join(HERE, "..", "internal", "discover", "pg_short.tsv.gz")

NS = {"rdf": "http://www.w3.org/1999/02/22-rdf-syntax-ns#", "dcterms": "http://purl.org/dc/terms/",
      "pgterms": "http://www.gutenberg.org/2009/pgterms/"}
ABOUT = "{%s}about" % NS["rdf"]

FICTION = {"Short Stories", "Category: Short Stories", "Science Fiction", "Category: Science-Fiction & Fantasy", "Fantasy",
           "Horror", "Gothic Fiction", "Precursors of Science Fiction", "Science Fiction by Women"}
ESSAYS = "Category: Essays, Letters & Speeches"
NOT_PROSE = {"Category: Plays/Films/Dramas", "Category: Poetry", "One Act Plays"}
MAGAZINES = {"Astounding Stories", "The Atlantic Monthly", "Blackwood's Edinburgh Magazine", "Lippincott's Magazine",
             "Harper's New Monthly Magazine", "Continental Monthly", "Chambers's Edinburgh Journal",
             "The International Magazine of Literature, Art, and Science", "The Bay State Monthly"}
COLLECTION = re.compile(r"\b(stories|tales|sketches|collection|anthology|omnibus|works|essays|papers|selections|"
                        r"miscellan\w*|volume|vol\.|no\. ?\d|number \d|part [ivx\d]+)\b", re.I)
NOT_ESSAY = re.compile(r"\b(letters?|correspondence|sermons?|speech(es)?|address(es)?|proclamation|report|catalogue|"
                       r"index|bibliograph\w*|dictionary|grammar|bible|testament)\b", re.I)
LICENCE = 19000  # bytes of Gutenberg's header and licence
MIN_WORDS, MAX_WORDS = 1000, 20000


def books(dump):
    with tarfile.open(dump, "r:bz2") as tar:
        for m in tar:
            if not m.isfile() or not m.name.endswith(".rdf"):
                continue
            eb = ET.parse(tar.extractfile(m)).getroot().find("pgterms:ebook", NS)
            if eb is None:
                continue
            langs = [v.text for v in eb.findall("dcterms:language/rdf:Description/rdf:value", NS)]
            if eb.findtext("dcterms:type/rdf:Description/rdf:value", "", NS) != "Text" or langs != ["en"]:
                continue
            size = 0
            for f in eb.findall("dcterms:hasFormat/pgterms:file", NS):
                if f.get(ABOUT, "").endswith(".txt.utf-8"):
                    size = int(f.findtext("dcterms:extent", "0", NS) or 0)
            if not size:
                continue
            yield {
                "n": eb.get(ABOUT).split("/")[-1],
                "title": " ".join((eb.findtext("dcterms:title", "", NS) or "").split()),
                "author": "; ".join(a.text for a in eb.findall("dcterms:creator/pgterms:agent/pgterms:name", NS)),
                "shelves": {v.text for v in eb.findall("pgterms:bookshelf/rdf:Description/rdf:value", NS)},
                "words": max(0, (size - LICENCE) // 6),
            }


def kind(b):
    shelves, title, words = b["shelves"], b["title"], b["words"]
    if any("Children" in s for s in shelves) or shelves & NOT_PROSE:
        return None
    if shelves & MAGAZINES:
        return "i"
    fiction = bool(shelves & FICTION)
    if fiction and COLLECTION.search(title):
        return "a" if words >= 3000 else None
    if not MIN_WORDS <= words <= MAX_WORDS:
        return None
    if fiction:
        return "s"
    if ESSAYS in shelves and not NOT_ESSAY.search(title) and not COLLECTION.search(title):
        return "e"
    return None


def main():
    data = sys.argv[1] if len(sys.argv) > 1 else tempfile.mkdtemp()
    dump = os.path.join(data, "rdf-files.tar.bz2")
    if not os.path.exists(dump):
        print("downloading", FEED)
        urllib.request.urlretrieve(FEED, dump)
    out, counts = [], {}
    for b in books(dump):
        k = kind(b)
        if k is None:
            continue
        counts[k] = counts.get(k, 0) + 1
        # "$b" is where the catalogue's title had its subtitle (MARC).
        clean = lambda s: s.replace(" : $b ", ": ").replace("$b", "").replace("\t", " ").replace("\n", " ")
        out.append((int(b["n"]), k, b["words"], clean(b["title"]), clean(b["author"])))
    out.sort()
    with gzip.open(OUT, "wt", encoding="utf-8", newline="\n") as w:
        w.write("# book\tkind\twords\ttitle\tauthor (scripts/pg_short.py)\n")
        for row in out:
            w.write("\t".join(map(str, row)) + "\n")
    print(counts, "->", OUT)


if __name__ == "__main__":
    main()

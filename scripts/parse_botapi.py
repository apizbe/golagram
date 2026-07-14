#!/usr/bin/env python3
"""
parse_botapi.py — Stage 1 of the golagram codegen pipeline.

Reads the raw HTML of https://core.telegram.org/bots/api and produces api.json:
a structured inventory of every section, type, method, field, parameter,
union, and return-value note in the current Bot API.

The Telegram docs page is machine-generated and extremely regular:

    <h3><a class="anchor" name="available-methods" ...></a>Available methods</h3>
    <h4><a class="anchor" name="sendmessage" ...></a>sendMessage</h4>
    <p>Use this method to send text messages. On success, the sent
       <a href="#message">Message</a> is returned.</p>
    <table class="table">
      <thead><tr><th>Parameter</th><th>Type</th><th>Required</th><th>Description</th></tr></thead>
      <tbody><tr><td>chat_id</td><td>Integer or String</td><td>Yes</td><td>...</td></tr>...</tbody>
    </table>

Rules used here:
  * h4 title with no spaces == an API item. lowercase first letter -> method,
    uppercase -> type. Titles with spaces ("Formatting options", changelog
    dates) are prose sections, kept only as context.
  * Table header "Field"     -> the item is a type, rows are fields.
  * Table header "Parameter" -> the item is a method, rows are params.
  * A <ul> of anchor links right after the description, with no table,
    marks a union type (e.g. MessageOrigin -> 4 concrete subtypes).
  * Sentences containing "eturn" in the description are the return-value
    contract for methods.
"""
import json
import re
import sys

def strip_tags(html: str) -> str:
    html = re.sub(r'<br\s*/?>', ' ', html)
    html = re.sub(r'<img[^>]*alt="([^"]*)"[^>]*>', r'\1', html)  # emoji images
    html = re.sub(r'<[^>]+>', '', html)
    # un-escape the handful of entities Telegram uses
    for ent, ch in [('&amp;', '&'), ('&lt;', '<'), ('&gt;', '>'),
                    ('&quot;', '"'), ('&#39;', "'"), ('&nbsp;', ' '),
                    ('&#8217;', "'"), ('&mdash;', '—'), ('&ndash;', '–')]:
        html = html.replace(ent, ch)
    return re.sub(r'\s+', ' ', html).strip()

def parse_table(table_html: str):
    """Return (kind, rows) where kind is 'fields' or 'params'."""
    header_cells = [strip_tags(c) for c in re.findall(
        r'<th[^>]*>(.*?)</th>', table_html, re.S)]
    rows = []
    for tr in re.findall(r'<tr[^>]*>(.*?)</tr>', table_html, re.S):
        cells = [strip_tags(c) for c in re.findall(r'<td[^>]*>(.*?)</td>', tr, re.S)]
        if cells:
            rows.append(cells)
    if header_cells and header_cells[0] == 'Parameter':
        kind = 'params'
        out = [{'name': r[0], 'type': r[1], 'required': r[2], 'description': r[3]}
               for r in rows if len(r) == 4]
    else:
        kind = 'fields'
        out = [{'name': r[0], 'type': r[1], 'description': r[2]}
               for r in rows if len(r) == 3]
    return kind, out

def main(path: str, out_path: str):
    html = open(path, encoding='utf-8').read()
    # cut to the docs body: from the first h3 to the page footer
    start = html.find('<h3>')
    end = html.find('dev_page_nav_wrap')
    if end == -1:
        end = len(html)
    body = html[start:end]

    # split into chunks, each beginning with an h3 or h4
    chunks = re.split(r'(?=<h[34]>)', body)

    items = []
    current_section = None
    api_version = None
    m = re.search(r'Bot API (\d+\.\d+)', html)
    if m:
        api_version = m.group(1)

    for chunk in chunks:
        hm = re.match(r'<h([34])><a class="anchor" name="([^"]+)"[^>]*>.*?</a>(.*?)</h\1>',
                      chunk, re.S)
        if not hm:
            continue
        level, anchor, raw_title = hm.groups()
        title = strip_tags(raw_title)
        rest = chunk[hm.end():]

        if level == '3':
            current_section = title
            continue

        # description: all <p> and <blockquote> before the first table
        first_table = rest.find('<table')
        desc_zone = rest if first_table == -1 else rest[:first_table]
        paras = re.findall(r'<p>(.*?)</p>', desc_zone, re.S)
        description = ' '.join(strip_tags(p) for p in paras).strip()

        item = {
            'section': current_section,
            'anchor': anchor,
            'name': title,
            'description': description,
        }

        if ' ' in title:            # prose subsection, not an API item
            item['kind'] = 'prose'
            items.append(item)
            continue

        is_method = title[0].islower()
        item['kind'] = 'method' if is_method else 'type'

        tm = re.search(r'<table[^>]*>(.*?)</table>', rest, re.S)
        if tm:
            kind, rows = parse_table(tm.group(1))
            item[kind] = rows

        # union detection: no table, but a list of type links
        if not tm:
            lis = re.findall(r'<li><a href="#[^"]+">([^<]+)</a></li>', desc_zone)
            if lis and not is_method:
                item['kind'] = 'union'
                item['members'] = lis

        if is_method:
            ret = [s.strip() for s in re.split(r'(?<=[.]) ', description)
                   if 'eturn' in s]
            item['returns'] = ' '.join(ret)

        items.append(item)

    result = {
        'api_version': api_version,
        'source': 'https://core.telegram.org/bots/api',
        'counts': {
            'types':   sum(1 for i in items if i['kind'] == 'type'),
            'unions':  sum(1 for i in items if i['kind'] == 'union'),
            'methods': sum(1 for i in items if i['kind'] == 'method'),
        },
        'items': items,
    }
    with open(out_path, 'w', encoding='utf-8') as f:
        json.dump(result, f, indent=1, ensure_ascii=False)
    print(f"Bot API {api_version}: {result['counts']}")

if __name__ == '__main__':
    main(sys.argv[1], sys.argv[2])

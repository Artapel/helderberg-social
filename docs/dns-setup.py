#!/usr/bin/env python3
"""Point helderbergsocial.co.za at GitHub Pages through the HostAfrica public API.

Usage (token from the environment only, never in the file or argv):
    HA_TOKEN='<api token>' python docs/dns-setup.py            # apply
    HA_TOKEN='<api token>' python docs/dns-setup.py --dry-run  # show the plan only
    HA_TOKEN='<api token>' python docs/dns-setup.py --mail     # also publish SPF/DKIM/DMARC/null MX

Idempotent: adds only records that are missing, deletes only the registrar's parking A
record, repoints www. Leaves SOA, NS, mail and ftp records alone. With --mail it fetches
the records the API wants published from https://api.helderbergsocial.co.za/api/mail-dns
(or --mail-json <file> for a saved copy), adds the SPF, DKIM and DMARC TXT records and
replaces the MX with a null MX ("0 .", RFC 7505: the domain sends but never receives).
HostAfrica rejects "." as an MX exchange ("Supplied exchange for MX record is invalid",
seen 2026-09-04), in which case the MX is deleted instead: no MX means receivers fall back
to the A record, which is the web host and does not listen on 25, so the effect is the same.

API shapes from https://api.hostafrica.com/docs/ (embedded OpenAPI, read 2026-09-03):
    POST /dns/list-zones                            -> data.zones[{domain_id, zone_id, domain_name}]
    POST /dns/get-zone      {domain_id}             -> data.records[{id, name, type, content, ttl}]
    POST /dns/add-record    {zone_id, domain_name, record:{name,type,content,ttl}}
    POST /dns/edit-record   {zone_id, domain_name, record:{id,name,type,content,ttl}}
    POST /dns/delete-record {zone_id, domain_name, record:{id,name,type,content,ttl}}
"""
import json, os, sys, urllib.request

DOMAIN = "helderbergsocial.co.za"
GH_USER = "artapel"
API_HOST = "api"            # api.helderbergsocial.co.za -> the reverse proxy in front of the API container
API_IP = "41.221.5.39"
API = "https://api.hostafrica.com"
TTL = 14400
# From docs.github.com "Managing a custom domain for your GitHub Pages site", 2026-09-02.
A_RECORDS = ["185.199.108.153", "185.199.109.153", "185.199.110.153", "185.199.111.153"]
AAAA_RECORDS = ["2606:50c0:8000::153", "2606:50c0:8001::153", "2606:50c0:8002::153", "2606:50c0:8003::153"]

DRY = "--dry-run" in sys.argv
MAIL = "--mail" in sys.argv or "--mail-json" in sys.argv
MAIL_URL = "https://api.helderbergsocial.co.za/api/mail-dns"
TOKEN = os.environ.get("HA_TOKEN") or sys.exit("set HA_TOKEN in the environment")


def api(path, body):
    req = urllib.request.Request(API + path, data=json.dumps(body).encode(), method="POST",
                                 headers={"Authorization": "Bearer " + TOKEN, "Content-Type": "application/json", "Accept": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            return json.load(r)
    except urllib.error.HTTPError as e:
        sys.exit(f"{path} -> HTTP {e.code}: {e.read().decode()[:300]}")


def mutate(path, record, fatal=True):
    """Returns True on success. With fatal=False a failure is printed and returned instead."""
    if DRY:
        return True
    try:
        r = api(path, {"zone_id": zone_id, "domain_name": DOMAIN, "record": record})
    except SystemExit as e:
        if fatal:
            raise
        print(f"       FAILED: {e}")
        return False
    if r.get("status") != "success":
        if fatal:
            sys.exit(f"{path} failed: {json.dumps(r)[:300]}")
        print(f"       FAILED: {json.dumps(r)[:300]}")
        return False
    return True


def mail_records():
    """The records the API asks for, from /api/mail-dns or a saved JSON file."""
    if "--mail-json" in sys.argv:
        path = sys.argv[sys.argv.index("--mail-json") + 1]
        with open(path, encoding="utf-8") as f:
            data = json.load(f)
    else:
        with urllib.request.urlopen(MAIL_URL, timeout=20) as r:
            data = json.load(r)
    if data.get("domain") != DOMAIN:
        sys.exit(f"mail-dns is for {data.get('domain')!r}, not {DOMAIN}")
    return data["records"]


def txt_chunks(value):
    """A TXT string longer than 255 octets must be published as several quoted strings."""
    return " ".join('"%s"' % value[i:i + 255] for i in range(0, len(value), 255))


zones = api("/dns/list-zones", {})["data"]["zones"]
zone = next((z for z in zones if z["domain_name"] == DOMAIN), None) or sys.exit(f"no zone for {DOMAIN}: {zones}")
zone_id, domain_id = zone["zone_id"], zone["domain_id"]
records = api("/dns/get-zone", {"domain_id": domain_id})["data"]["records"]
print(f"zone {zone_id} ({domain_id}), {len(records)} records" + (" [dry run]" if DRY else ""))

def have(name, rtype, content):
    return any(r["name"] == name and r["type"] == rtype and r["content"] == content for r in records)

for rtype, values in (("A", A_RECORDS), ("AAAA", AAAA_RECORDS)):
    for ip in values:
        if have("@", rtype, ip):
            print(f"keep   {rtype:5} @   -> {ip}")
        else:
            print(f"add    {rtype:5} @   -> {ip}")
            mutate("/dns/add-record", {"name": "@", "type": rtype, "content": ip, "ttl": TTL})

for r in records:
    if r["type"] == "A" and r["name"] == "@" and not r["content"].startswith("185.199."):
        print(f"delete A     @   -> {r['content']}  (parking)")
        mutate("/dns/delete-record", r)

www = next((r for r in records if r["type"] == "CNAME" and r["name"] == "www"), None)
target = GH_USER + ".github.io"
if www and www["content"] == target:
    print(f"keep   CNAME www -> {target}")
elif www:
    print(f"edit   CNAME www -> {target}  (was {www['content']})")
    mutate("/dns/edit-record", dict(www, content=target))
else:
    print(f"add    CNAME www -> {target}")
    mutate("/dns/add-record", {"name": "www", "type": "CNAME", "content": target, "ttl": TTL})

apirec = next((r for r in records if r["type"] == "A" and r["name"] == API_HOST), None)
if apirec and apirec["content"] == API_IP:
    print(f"keep   A     {API_HOST} -> {API_IP}")
elif apirec:
    print(f"edit   A     {API_HOST} -> {API_IP}  (was {apirec['content']})")
    mutate("/dns/edit-record", dict(apirec, content=API_IP))
else:
    print(f"add    A     {API_HOST} -> {API_IP}")
    mutate("/dns/add-record", {"name": API_HOST, "type": "A", "content": API_IP, "ttl": TTL})

if MAIL:
    print("\nmail records:")
    wanted = mail_records()
    # HostAfrica shows the zone apex as "@" and sub-names relative to the zone.
    def rel(name):
        return "@" if name == DOMAIN else name[:-len("." + DOMAIN)] if name.endswith("." + DOMAIN) else name
    def norm(v):
        return v.replace('" "', "").strip('"').replace(" ", "")
    for w in wanted:
        name, rtype, value = rel(w["Name"]), w["Type"], w["Value"]
        if rtype == "TXT":
            tag = value.lower().split(";")[0].split(" ")[0]      # v=spf1 / v=dkim1 / v=dmarc1
            existing = [r for r in records if r["type"] == "TXT" and r["name"] == name and r["content"].lower().lstrip('"').startswith(tag)]
            if any(norm(r["content"]) == norm(value) for r in existing):
                print(f"keep   TXT   {name} -> {value[:60]}{'...' if len(value) > 60 else ''}")
                continue
            if existing:
                print(f"edit   TXT   {name} -> {value[:60]}{'...' if len(value) > 60 else ''}  (was {existing[0]['content'][:40]}...)")
                ok = mutate("/dns/edit-record", dict(existing[0], content=value), fatal=False)
                if not ok and len(value) > 255:
                    print("       retrying as 255-octet quoted strings")
                    mutate("/dns/edit-record", dict(existing[0], content=txt_chunks(value)), fatal=False)
                continue
            print(f"add    TXT   {name} -> {value[:60]}{'...' if len(value) > 60 else ''}")
            ok = mutate("/dns/add-record", {"name": name, "type": "TXT", "content": value, "ttl": TTL}, fatal=False)
            if not ok and len(value) > 255:
                print("       retrying as 255-octet quoted strings")
                mutate("/dns/add-record", {"name": name, "type": "TXT", "content": txt_chunks(value), "ttl": TTL}, fatal=False)
        elif rtype == "MX":
            mxs = [r for r in records if r["type"] == "MX" and r["name"] == name]
            if any(r["content"].replace(" ", "") in ("0.", ".") for r in mxs) and len(mxs) == 1:
                print(f"keep   MX    {name} -> {mxs[0]['content']}  (null MX)")
                continue
            if not mxs:
                print(f"add    MX    {name} -> 0 .  (null MX)")
                if not mutate("/dns/add-record", {"name": name, "type": "MX", "content": "0 .", "ttl": TTL}, fatal=False):
                    print("       registrar rejects a null MX; leaving the zone with no MX, which has the same effect")
                continue
            print(f"edit   MX    {name} -> 0 .  (null MX; was {mxs[0]['content']})")
            if mutate("/dns/edit-record", dict(mxs[0], content="0 ."), fatal=False):
                mxs = mxs[1:]
            else:
                print("       registrar rejects a null MX; deleting the MX instead (no MX = replies fall back to the web host's A record and fail fast)")
            for extra in mxs:
                print(f"delete MX    {name} -> {extra['content']}")
                mutate("/dns/delete-record", extra, fatal=False)
        else:
            print(f"skip   {rtype:5} {name} (not handled)")

if not DRY:
    after = api("/dns/get-zone", {"domain_id": domain_id})["data"]["records"]
    print("\nzone now:")
    for r in sorted(after, key=lambda r: (r["type"], r["name"], r["content"])):
        print(f"  {r['type']:5} {r['name']:15} {r['content'][:100]}")

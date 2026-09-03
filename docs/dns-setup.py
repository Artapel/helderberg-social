#!/usr/bin/env python3
"""Point helderbergsocial.co.za at GitHub Pages through the HostAfrica public API.

Usage (token from the environment only, never in the file or argv):
    HA_TOKEN='<api token>' python docs/dns-setup.py            # apply
    HA_TOKEN='<api token>' python docs/dns-setup.py --dry-run  # show the plan only

Idempotent: adds only records that are missing, deletes only the registrar's parking A
record, repoints www. Leaves SOA, NS, MX, mail and ftp records alone.

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
API = "https://api.hostafrica.com"
TTL = 14400
# From docs.github.com "Managing a custom domain for your GitHub Pages site", 2026-09-02.
A_RECORDS = ["185.199.108.153", "185.199.109.153", "185.199.110.153", "185.199.111.153"]
AAAA_RECORDS = ["2606:50c0:8000::153", "2606:50c0:8001::153", "2606:50c0:8002::153", "2606:50c0:8003::153"]

DRY = "--dry-run" in sys.argv
TOKEN = os.environ.get("HA_TOKEN") or sys.exit("set HA_TOKEN in the environment")


def api(path, body):
    req = urllib.request.Request(API + path, data=json.dumps(body).encode(), method="POST",
                                 headers={"Authorization": "Bearer " + TOKEN, "Content-Type": "application/json", "Accept": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            return json.load(r)
    except urllib.error.HTTPError as e:
        sys.exit(f"{path} -> HTTP {e.code}: {e.read().decode()[:300]}")


def mutate(path, record):
    if DRY:
        return
    r = api(path, {"zone_id": zone_id, "domain_name": DOMAIN, "record": record})
    if r.get("status") != "success":
        sys.exit(f"{path} failed: {json.dumps(r)[:300]}")


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

if not DRY:
    after = api("/dns/get-zone", {"domain_id": domain_id})["data"]["records"]
    print("\nzone now:")
    for r in sorted(after, key=lambda r: (r["type"], r["name"], r["content"])):
        print(f"  {r['type']:5} {r['name']:5} {r['content']}")
